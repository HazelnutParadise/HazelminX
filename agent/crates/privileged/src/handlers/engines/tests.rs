use super::*;
use crate::BoxFuture;
use crate::command::{CommandOutput, output};
use std::io;
use std::os::unix::fs::PermissionsExt;
use std::sync::Mutex;
use tokio::net::UnixListener;

/// Answers by program file name and first argument; anything else fails.
struct Fake {
    replies: Vec<(&'static str, &'static str, CommandOutput)>,
    calls: Mutex<Vec<CommandSpec>>,
}

impl Fake {
    fn new(replies: Vec<(&'static str, &'static str, CommandOutput)>) -> Arc<Fake> {
        Arc::new(Fake {
            replies,
            calls: Mutex::default(),
        })
    }
}

impl CommandRunner for Fake {
    fn run(
        &self,
        spec: CommandSpec,
        _sink: OutputSink,
    ) -> BoxFuture<'static, io::Result<CommandOutput>> {
        let name = spec
            .program
            .file_name()
            .unwrap()
            .to_string_lossy()
            .into_owned();
        let first = spec
            .args
            .first()
            .map(|a| a.to_string_lossy().into_owned())
            .unwrap_or_default();
        self.calls.lock().unwrap().push(spec);
        let r = self
            .replies
            .iter()
            .find(|(p, a, _)| *p == name && *a == first)
            .map(|(_, _, o)| o.clone());
        Box::pin(async move { r.ok_or_else(|| io::Error::from(io::ErrorKind::NotFound)) })
    }
}

const SHOW: &str = "Id=docker.service\nLoadState=loaded\nActiveState=active\nSubState=running\nUnitFileState=enabled\n\nId=docker.socket\nLoadState=loaded\nActiveState=active\nSubState=listening\nUnitFileState=enabled\n\nId=containerd.service\nLoadState=not-found\nActiveState=inactive\nSubState=dead\nUnitFileState=\n";

fn touch_exe(root: &Path, abs: &str) {
    let p = root.join(abs.trim_start_matches('/'));
    std::fs::create_dir_all(p.parent().unwrap()).unwrap();
    std::fs::write(&p, "").unwrap();
    std::fs::set_permissions(&p, std::fs::Permissions::from_mode(0o755)).unwrap();
}

/// Serves `version` and `info` bodies over a Unix socket at `abs`.
fn serve(root: &Path, abs: &str, version: &'static str, info: &'static str) -> PathBuf {
    let p = root.join(abs.trim_start_matches('/'));
    std::fs::create_dir_all(p.parent().unwrap()).unwrap();
    let l = UnixListener::bind(&p).unwrap();
    tokio::spawn(async move {
        loop {
            let Ok((mut s, _)) = l.accept().await else {
                return;
            };
            tokio::spawn(async move {
                let mut buf = vec![0u8; 1024];
                let n = s.read(&mut buf).await.unwrap_or(0);
                let req = String::from_utf8_lossy(&buf[..n]).into_owned();
                let body = if req.starts_with("GET /version") {
                    version
                } else {
                    info
                };
                let _ = s
                    .write_all(
                        format!("HTTP/1.0 200 OK\r\nContent-Type: application/json\r\n\r\n{body}")
                            .as_bytes(),
                    )
                    .await;
            });
        }
    });
    p
}

const DOCKER_VERSION: &str = r#"{"Platform":{"Name":"Docker Engine - Community"},"Components":[{"Name":"Engine","Version":"27.3.1"}],"Version":"27.3.1","ApiVersion":"1.47"}"#;
const DOCKER_INFO: &str = r#"{"Containers":5,"ContainersRunning":2,"CgroupDriver":"systemd","CgroupVersion":"2","Swarm":{"LocalNodeState":"inactive"}}"#;
const PODMAN_VERSION: &str = r#"{"Platform":{"Name":"linux/amd64/debian-13"},"Components":[{"Name":"Podman Engine","Version":"5.4.2"}],"Version":"5.4.2","ApiVersion":"1.41"}"#;
const PODMAN_INFO: &str =
    r#"{"Containers":1,"ContainersRunning":0,"CgroupDriver":"systemd","CgroupVersion":"2"}"#;
const OLD_DOCKER: &str = r#"{"Version":"19.03.15","ApiVersion":"1.40"}"#;

fn uid() -> u32 {
    std::fs::metadata("/proc/self")
        .map(|m| m.uid())
        .unwrap_or_else(|_| {
            // macOS has no /proc: the owner of a fresh file is this process.
            let f = tempfile::NamedTempFile::new().unwrap();
            f.as_file().metadata().unwrap().uid()
        })
}

#[test]
fn kind_comes_from_the_answer_not_the_path() {
    let v: serde_json::Value = serde_json::from_str(DOCKER_VERSION).unwrap();
    assert_eq!(answered_kind(&v), Some(kind::DOCKER));
    let v: serde_json::Value = serde_json::from_str(PODMAN_VERSION).unwrap();
    assert_eq!(answered_kind(&v), Some(kind::PODMAN));
    let v: serde_json::Value =
        serde_json::from_str(r#"{"Platform":{"Name":"Podman Engine"},"Version":"4.3.1"}"#).unwrap();
    assert_eq!(answered_kind(&v), Some(kind::PODMAN));
    assert_eq!(answered_kind(&serde_json::json!({"hello": 1})), None);
    assert_eq!(answered_kind(&serde_json::json!({"Version": "1"})), None);
}

#[test]
fn versions_and_http_and_show_parse() {
    assert_eq!(major_minor("1.47"), Some((1, 47)));
    assert_eq!(major_minor("v4.9.3"), Some((4, 9)));
    assert_eq!(major_minor("5-dev"), Some((5, 0)));
    assert_eq!(major_minor("x"), None);
    assert!(parse_http(b"HTTP/1.0 200 OK\r\n\r\n{\"a\":1}").is_ok());
    assert_eq!(
        parse_http(b"HTTP/1.0 404 Not Found\r\n\r\n{}").unwrap_err(),
        "HTTP status 404"
    );
    assert!(parse_http(b"SSH-2.0-OpenSSH\r\n\r\n").is_err());
    assert!(parse_http(b"HTTP/1.0 200 OK\r\n\r\nnot json").is_err());
    let u = parse_show(SHOW);
    assert_eq!(u.len(), 3);
    assert_eq!(u[0].active_state.as_deref(), Some("active"));
    assert_eq!(u[2].load_state.as_deref(), Some("not-found"));
    assert_eq!(u[2].unit_file_state, None, "empty is not provided");
}

#[test]
fn engine_states_are_told_apart() {
    let unit = |load: &str, active: &str| Unit {
        name: "docker.service".into(),
        load_state: Some(load.into()),
        active_state: Some(active.into()),
        sub_state: None,
        unit_file_state: None,
    };
    let none: &[Environment] = &[];
    assert_eq!(
        engine_state(
            kind::DOCKER,
            false,
            Some(&[unit("not-found", "inactive")]),
            none
        ),
        state::NOT_INSTALLED
    );
    assert_eq!(
        engine_state(
            kind::DOCKER,
            true,
            Some(&[unit("loaded", "inactive")]),
            none
        ),
        state::STOPPED
    );
    assert_eq!(
        engine_state(kind::DOCKER, true, Some(&[unit("loaded", "active")]), none),
        state::RUNNING
    );
    assert_eq!(
        engine_state(kind::DOCKER, true, None, none),
        state::UNKNOWN,
        "no systemd: not guessed"
    );
    let mut env = Environment {
        id: "env:/run/docker.sock".into(),
        kind: kind::DOCKER.into(),
        kind_confirmed: true,
        endpoint: "/run/docker.sock".into(),
        endpoint_resolved: "/run/docker.sock".into(),
        mode: "rootful".into(),
        owner_uid: 0,
        owner_name: None,
        state: state::INCOMPATIBLE.into(),
        version: None,
        api_version: None,
        cgroup_driver: None,
        cgroup_version: None,
        swarm: None,
        containers: None,
        containers_running: None,
        error: None,
    };
    assert_eq!(
        engine_state(
            kind::DOCKER,
            true,
            Some(&[unit("loaded", "active")]),
            &[env.clone()]
        ),
        state::INCOMPATIBLE
    );
    env.state = state::PERMISSION_DENIED.into();
    assert_eq!(
        engine_state(kind::DOCKER, true, None, &[env.clone()]),
        state::PERMISSION_DENIED
    );
    assert_eq!(
        engine_state(kind::PODMAN, false, Some(&[]), &[env]),
        state::NOT_INSTALLED,
        "other kinds do not count"
    );
}

#[tokio::test]
async fn empty_host_reports_every_engine_not_installed() {
    let root = tempfile::tempdir().unwrap();
    let fake = Fake::new(vec![(
        "systemctl",
        "show",
        output(
            0,
            "Id=docker.service\nLoadState=not-found\nActiveState=inactive\n",
        ),
    )]);
    let r = inspect(fake, root.path()).await;
    assert_eq!(r.engines.len(), 3);
    for e in &r.engines {
        assert_eq!(e.state, state::NOT_INSTALLED, "{}", e.kind);
        assert_eq!(e.binary, None);
        assert_eq!(e.binary_version, None);
    }
    assert!(r.environments.is_empty());
}

#[tokio::test]
async fn podman_docker_socket_is_podman_and_rootless_envs_stay_apart() {
    let root = tempfile::tempdir().unwrap();
    let me = uid();
    std::fs::create_dir_all(root.path().join("etc")).unwrap();
    std::fs::write(
        root.path().join("etc/passwd"),
        format!("root:x:0:0::/root:/bin/sh\ntim:x:{me}:{me}::/home/tim:/bin/sh\n"),
    )
    .unwrap();
    touch_exe(root.path(), "/usr/bin/docker");
    touch_exe(root.path(), "/usr/bin/podman");
    touch_exe(
        root.path(),
        "/usr/libexec/docker/cli-plugins/docker-compose",
    );
    // podman-docker: docker.sock is served by Podman.
    serve(root.path(), "/run/docker.sock", PODMAN_VERSION, PODMAN_INFO);
    // var/run → run, as on every current distribution.
    std::fs::create_dir_all(root.path().join("var")).unwrap();
    std::os::unix::fs::symlink("../run", root.path().join("var/run")).unwrap();
    // The test user's rootless Docker and Podman.
    serve(
        root.path(),
        &format!("/run/user/{me}/docker.sock"),
        DOCKER_VERSION,
        DOCKER_INFO,
    );
    serve(
        root.path(),
        &format!("/run/user/{me}/podman/podman.sock"),
        PODMAN_VERSION,
        PODMAN_INFO,
    );
    let fake = Fake::new(vec![
        ("systemctl", "show", output(0, SHOW)),
        (
            "docker",
            "--version",
            output(0, "Docker version 27.3.1, build ce12230\n"),
        ),
        ("podman", "--version", output(0, "\npodman version 5.4.2\n")),
        (
            "docker-compose",
            "version",
            output(0, "Docker Compose version v2.29.7\n"),
        ),
    ]);
    let r = inspect(fake.clone(), root.path()).await;

    let ids: Vec<&str> = r.environments.iter().map(|e| e.id.as_str()).collect();
    assert_eq!(
        r.environments.len(),
        3,
        "{ids:?}: /var/run and /run are one socket"
    );
    let sys = &r.environments[0];
    assert_eq!(sys.kind, kind::PODMAN, "ENV-002: answered as Podman");
    assert!(sys.kind_confirmed);
    assert_eq!(sys.endpoint, "/var/run/docker.sock");
    assert_eq!(sys.endpoint_resolved, "/run/docker.sock");
    assert_eq!(sys.version.as_deref(), Some("5.4.2"));
    let mine: Vec<&Environment> = r
        .environments
        .iter()
        .filter(|e| e.owner_uid == me && e.endpoint.starts_with("/run/user/"))
        .collect();
    if me != 0 {
        assert_eq!(mine.len(), 3 - 1);
        assert!(
            mine.iter()
                .all(|e| e.mode == "rootless" && e.owner_name.as_deref() == Some("tim"))
        );
    }
    let docker = r
        .environments
        .iter()
        .find(|e| e.kind == kind::DOCKER)
        .unwrap();
    assert_eq!(docker.state, state::RUNNING);
    assert_eq!(docker.containers, Some(5));
    assert_eq!(docker.containers_running, Some(2));
    assert_eq!(docker.swarm.as_deref(), Some("inactive"));
    assert_eq!(docker.cgroup_driver.as_deref(), Some("systemd"));

    let d = &r.engines[0];
    assert_eq!(d.kind, kind::DOCKER);
    assert_eq!(d.state, state::RUNNING);
    assert_eq!(d.binary.as_deref(), Some("/usr/bin/docker"));
    assert_eq!(
        d.binary_version.as_deref(),
        Some("Docker version 27.3.1, build ce12230")
    );
    assert_eq!(d.plugins.len(), 1);
    assert_eq!(
        d.plugins[0].version.as_deref(),
        Some("Docker Compose version v2.29.7")
    );
    assert_eq!(d.units.len(), 3);
    assert_eq!(
        r.engines[1].binary_version.as_deref(),
        Some("podman version 5.4.2")
    );
    assert_eq!(r.engines[2].state, state::NOT_INSTALLED);
    // Every program run is a fixed path with a fixed argument.
    for c in fake.calls.lock().unwrap().iter() {
        assert!(c.program.is_absolute());
    }
}

#[tokio::test]
async fn old_engine_is_incompatible_and_silent_socket_is_not_responding() {
    let root = tempfile::tempdir().unwrap();
    touch_exe(root.path(), "/usr/bin/docker");
    serve(root.path(), "/run/docker.sock", OLD_DOCKER, "{}");
    // A socket nobody accepts on.
    let p = root.path().join("run/podman/podman.sock");
    std::fs::create_dir_all(p.parent().unwrap()).unwrap();
    let silent = std::os::unix::net::UnixListener::bind(&p).unwrap();
    drop(silent); // file stays, nothing listens: connection refused
    let fake = Fake::new(vec![("systemctl", "show", output(0, SHOW))]);
    let r = inspect(fake, root.path()).await;
    let d = r
        .environments
        .iter()
        .find(|e| e.kind == kind::DOCKER)
        .unwrap();
    assert_eq!(d.state, state::INCOMPATIBLE);
    assert_eq!(d.api_version.as_deref(), Some("1.40"));
    let p = r
        .environments
        .iter()
        .find(|e| e.endpoint == "/run/podman/podman.sock")
        .unwrap();
    assert_eq!(p.state, state::NOT_RESPONDING);
    assert!(!p.kind_confirmed, "kind not confirmed without an answer");
    assert!(p.error.is_some());
    assert_eq!(r.engines[0].state, state::INCOMPATIBLE);
    assert_eq!(r.engines[1].state, state::NOT_RESPONDING);
}

#[tokio::test]
async fn a_users_link_to_another_socket_is_not_followed() {
    let root = tempfile::tempdir().unwrap();
    serve(root.path(), "/run/docker.sock", DOCKER_VERSION, DOCKER_INFO);
    let dir = root.path().join("run/user/4242");
    std::fs::create_dir_all(&dir).unwrap();
    serve(root.path(), "/run/other.sock", DOCKER_VERSION, DOCKER_INFO);
    std::os::unix::fs::symlink(root.path().join("run/other.sock"), dir.join("docker.sock"))
        .unwrap();
    // A link to a socket already listed is the same environment, not a finding.
    std::fs::create_dir_all(dir.join("podman")).unwrap();
    std::os::unix::fs::symlink(
        root.path().join("run/docker.sock"),
        dir.join("podman/podman.sock"),
    )
    .unwrap();
    let fake = Fake::new(vec![]);
    let r = inspect(fake, root.path()).await;
    assert_eq!(r.environments.len(), 1);
    assert!(!r.uninspected.iter().any(|u| u.path.contains("podman.sock")));
    assert!(
        r.uninspected
            .iter()
            .any(|u| u.path == "/run/user/4242/docker.sock")
    );
    assert!(
        r.uninspected.iter().any(|u| u.path == SYSTEMCTL),
        "no systemctl answer is reported, not read as stopped"
    );
    assert_eq!(r.engines[0].state, state::RUNNING);
}

#[tokio::test]
async fn huge_or_endless_answers_are_bounded() {
    let root = tempfile::tempdir().unwrap();
    let p = root.path().join("run/docker.sock");
    std::fs::create_dir_all(p.parent().unwrap()).unwrap();
    let l = UnixListener::bind(&p).unwrap();
    tokio::spawn(async move {
        while let Ok((mut s, _)) = l.accept().await {
            tokio::spawn(async move {
                let mut b = [0u8; 256];
                let _ = s.read(&mut b).await;
                let _ = s.write_all(b"HTTP/1.0 200 OK\r\n\r\n{\"Version\":\"").await;
                let chunk = vec![b'a'; 64 * 1024];
                for _ in 0..40 {
                    if s.write_all(&chunk).await.is_err() {
                        return;
                    }
                }
            });
        }
    });
    let r = inspect(Fake::new(vec![]), root.path()).await;
    let e = &r.environments[0];
    assert_eq!(e.state, state::NOT_RESPONDING);
    assert!(
        e.error.as_deref().unwrap().contains("larger than"),
        "{:?}",
        e.error
    );
}

#[tokio::test]
async fn a_socket_served_by_another_uid_is_not_believed() {
    if uid() != 0 {
        return; // needs chown; runs in the Linux CI container as root
    }
    let root = tempfile::tempdir().unwrap();
    let p = serve(root.path(), "/run/docker.sock", DOCKER_VERSION, DOCKER_INFO);
    // The file says uid 4242, but root answers: what a swapped link looks like.
    std::os::unix::fs::chown(&p, Some(4242), None).unwrap();
    let r = inspect(Fake::new(vec![]), root.path()).await;
    let e = &r.environments[0];
    assert_eq!(e.state, state::NOT_RESPONDING);
    assert!(
        e.error.as_deref().unwrap().contains("not the socket owner"),
        "{:?}",
        e.error
    );
    assert_eq!(e.version, None);
}

#[test]
fn invisible_and_bidi_characters_are_dropped() {
    assert_eq!(cut("27.3\u{202E}1.\u{200B}x\u{0007}"), "27.31.x");
}

#[tokio::test]
async fn podman_docker_wrapper_is_not_docker_and_ids_stay_put() {
    let root = tempfile::tempdir().unwrap();
    touch_exe(root.path(), "/usr/bin/docker");
    touch_exe(root.path(), "/usr/bin/podman");
    std::fs::create_dir_all(root.path().join("run/podman")).unwrap();
    let real = serve(
        root.path(),
        "/run/podman/podman.sock",
        PODMAN_VERSION,
        PODMAN_INFO,
    );
    std::os::unix::fs::symlink(&real, root.path().join("run/docker.sock")).unwrap();
    let fake = || {
        Fake::new(vec![
            (
                "systemctl",
                "show",
                output(
                    0,
                    "Id=docker.service\nLoadState=not-found\nActiveState=inactive\n",
                ),
            ),
            ("docker", "--version", output(0, "podman version 5.4.2\n")),
            ("podman", "--version", output(0, "podman version 5.4.2\n")),
        ])
    };
    let r = inspect(fake(), root.path()).await;
    assert_eq!(r.environments.len(), 1, "one socket, two paths");
    assert_eq!(r.environments[0].id, "env:/run/podman/podman.sock");
    assert_eq!(
        r.engines[0].state,
        state::NOT_INSTALLED,
        "the wrapper is not Docker"
    );
    // The service stops: same id, kind still Podman, not reported as Docker.
    std::fs::remove_file(&real).unwrap();
    let _l = std::os::unix::net::UnixListener::bind(&real).unwrap();
    drop(_l);
    let r = inspect(fake(), root.path()).await;
    assert_eq!(r.environments[0].id, "env:/run/podman/podman.sock");
    assert_eq!(r.environments[0].kind, kind::PODMAN);
    assert_eq!(r.environments[0].state, state::NOT_RESPONDING);
}
