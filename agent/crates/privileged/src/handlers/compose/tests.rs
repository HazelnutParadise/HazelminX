//! Tests of the Compose handler that need no engine.

use super::*;
use std::os::unix::fs::PermissionsExt;

fn sha(b: &[u8]) -> String {
    hex::encode(Sha256::digest(b))
}

fn manifest(files: &[(&str, &[u8])]) -> ComposeManifest {
    serde_json::from_value(json!({
        "project": "shop-prod-web",
        "revision": "r1",
        "environment": "env:/var/run/docker.sock",
        "main_file": files[0].0,
        "files": files.iter().map(|(p, c)| json!({"path": p, "size": c.len(), "sha256": sha(c)})).collect::<Vec<_>>(),
    }))
    .unwrap()
}

/// A spool owned by this process's uid, laid out as the agent writes it.
fn spool(files: &[(&str, &[u8])]) -> (tempfile::TempDir, ComposeConfig) {
    let d = tempfile::tempdir().unwrap();
    let spool = d.path().join("compose-spool");
    let rev = spool.join("r1");
    std::fs::create_dir_all(&rev).unwrap();
    for p in [&spool, &rev] {
        std::fs::set_permissions(p, std::fs::Permissions::from_mode(0o700)).unwrap();
    }
    for (_, c) in files {
        std::fs::write(rev.join(sha(c)), c).unwrap();
    }
    let uid = unsafe { libc::geteuid() };
    (d, ComposeConfig { spool_dir: spool, agent_uid: Some(uid) })
}

const FILES: &[(&str, &[u8])] = &[("compose.yaml", b"services: {}\n"), ("conf/app.conf", b"listen 80;\n")];

#[test]
fn reads_the_files_the_manifest_signed() {
    let (_d, cfg) = spool(FILES);
    let got = read_spool(&cfg, &manifest(FILES)).unwrap();
    assert_eq!(got, vec![b"services: {}\n".to_vec(), b"listen 80;\n".to_vec()]);
}

#[test]
fn refuses_a_staged_file_that_was_changed_or_swapped_for_a_link() {
    let (_d, cfg) = spool(FILES);
    let m = manifest(FILES);
    let rev = cfg.spool_dir.join("r1");
    std::fs::write(rev.join(sha(b"listen 80;\n")), b"listen 81;\n").unwrap();
    assert!(read_spool(&cfg, &m).unwrap_err().contains("does not match"));
    std::fs::remove_file(rev.join(sha(b"listen 80;\n"))).unwrap();
    std::os::unix::fs::symlink("/etc/passwd", rev.join(sha(b"listen 80;\n"))).unwrap();
    let e = read_spool(&cfg, &m).unwrap_err();
    assert!(e.contains("staged file conf/app.conf"), "{e}");
}

#[test]
fn refuses_a_spool_that_is_a_link_writable_by_others_or_not_the_agents() {
    let (d, cfg) = spool(FILES);
    let m = manifest(FILES);
    let rev = cfg.spool_dir.join("r1");
    let moved = d.path().join("elsewhere");
    std::fs::rename(&rev, &moved).unwrap();
    std::os::unix::fs::symlink(&moved, &rev).unwrap();
    assert!(read_spool(&cfg, &m).is_err());
    std::fs::remove_file(&rev).unwrap();
    std::fs::rename(&moved, &rev).unwrap();
    std::fs::set_permissions(&rev, std::fs::Permissions::from_mode(0o777)).unwrap();
    assert!(read_spool(&cfg, &m).unwrap_err().contains("writable by others"));
    std::fs::set_permissions(&rev, std::fs::Permissions::from_mode(0o700)).unwrap();
    let other = ComposeConfig { agent_uid: Some(unsafe { libc::geteuid() } + 1), ..cfg.clone() };
    assert!(read_spool(&other, &m).unwrap_err().contains("not the agent"));
    let none = ComposeConfig { agent_uid: None, ..cfg };
    assert!(read_spool(&none, &m).is_err());
}

#[test]
fn lays_out_files_under_a_private_directory() {
    let d = tempfile::tempdir().unwrap();
    let m = manifest(FILES);
    let base = d.path().join("rev");
    root_dir(&base, 0o700).unwrap();
    write_files(&base, &m, &[b"services: {}\n".to_vec(), b"listen 80;\n".to_vec()]).unwrap();
    assert_eq!(std::fs::read(base.join("conf/app.conf")).unwrap(), b"listen 80;\n");
    assert_eq!(std::fs::metadata(base.join("conf")).unwrap().permissions().mode() & 0o777, 0o700);
    assert!(same_files(&base, &m));
    assert!(write_files(&base, &m, &[b"x".to_vec(), b"y".to_vec()]).is_err());
}

#[test]
fn a_directory_writable_by_others_or_a_link_is_not_used() {
    let d = tempfile::tempdir().unwrap();
    let open = d.path().join("open");
    std::fs::create_dir(&open).unwrap();
    std::fs::set_permissions(&open, std::fs::Permissions::from_mode(0o777)).unwrap();
    assert!(root_dir(&open, 0o700).is_err());
    let link = d.path().join("link");
    std::os::unix::fs::symlink(d.path(), &link).unwrap();
    assert!(root_dir(&link, 0o700).is_err());
}

#[test]
fn reads_images_ports_and_binds_from_compose_config() {
    let v = json!({"name": "shop-prod-web", "services": {
        "web": {"image": "nginx:1.27", "ports": [
            {"mode": "ingress", "target": 80, "published": "8080", "protocol": "tcp"},
            {"target": 53, "published": "5353", "protocol": "udp", "host_ip": "127.0.0.1"},
            {"target": 9000}],
            "volumes": [
                {"type": "bind", "source": "/srv/rev/conf/app.conf", "target": "/etc/app.conf"},
                {"type": "bind", "source": "/srv/rev/data", "target": "/data"},
                {"type": "volume", "source": "db", "target": "/var/lib/db"}]},
        "api": {"build": {"context": "/srv/rev"}, "image": "shop-api"}
    }});
    let r = parse_config(&v).unwrap();
    let web = r.services.iter().find(|s| s.name == "web").unwrap();
    assert_eq!(web.ports, vec![("tcp".into(), 8080, "".into()), ("udp".into(), 5353, "127.0.0.1".into())]);
    assert_eq!(web.binds.len(), 2);
    assert!(r.services.iter().any(|s| s.name == "api" && s.build));
    let m = manifest(FILES);
    assert_eq!(data_dirs(Path::new("/srv/rev"), &m, &r), vec!["data".to_string()]);
}

#[test]
fn reads_containers_and_their_published_ports() {
    let v = json!([
        {"Id": "b2", "Names": ["/shop-prod-web-web-1"], "Image": "nginx:1.27", "ImageID": "sha256:1", "State": "running",
         "Labels": {"com.docker.compose.service": "web"}, "Ports": [{"PrivatePort": 80, "PublicPort": 8080, "Type": "tcp"}]},
        {"Id": "a1", "Names": ["/shop-prod-web-db-1"], "Image": "postgres:17", "State": "exited", "Labels": {}}
    ]);
    let s = parse_containers(&v).unwrap();
    assert!(s.exists);
    assert_eq!(s.containers[0].name, "shop-prod-web-db-1");
    assert_eq!(s.containers[1].service, "web");
    assert!(own_ports(&v).contains(&("tcp".to_string(), 8080)));
    assert!(!parse_containers(&json!([])).unwrap().exists);
}

#[tokio::test]
async fn compose_runs_with_fixed_arguments() {
    use crate::command::{RecordingRunner, output};
    let runner = Arc::new(RecordingRunner::with_replies([output(0, r#"{"services":{"web":{"image":"x"}}}"#)]));
    let m = manifest(FILES);
    let dyn_runner: Arc<dyn CommandRunner> = runner.clone();
    let r = resolve(&dyn_runner, Path::new("/usr/bin/docker"), Path::new("/var/run/docker.sock"), Path::new("/tmp/rev"), &m)
        .await
        .unwrap();
    assert_eq!(r.services[0].name, "web");
    let call = &runner.calls()[0];
    let args: Vec<String> = call.args.iter().map(|a| a.to_string_lossy().into_owned()).collect();
    assert_eq!(
        args,
        [
            "compose", "--progress", "plain", "--project-name", "shop-prod-web", "--project-directory", "/tmp/rev",
            "--file", "/tmp/rev/compose.yaml", "config", "--format", "json"
        ]
    );
    assert!(call.env.iter().any(|(k, v)| k == "DOCKER_HOST" && v == "unix:///var/run/docker.sock"));
}

#[test]
fn engine_socket_must_be_a_root_socket_not_a_link() {
    let d = tempfile::tempdir().unwrap();
    let env: EnvironmentId = "env:/var/run/docker.sock".to_string().try_into().unwrap();
    assert!(engine_socket(d.path(), &env).unwrap_err().contains("engine socket"));
    std::fs::create_dir_all(d.path().join("var/run")).unwrap();
    std::fs::write(d.path().join("var/run/docker.sock"), b"").unwrap();
    assert!(engine_socket(d.path(), &env).unwrap_err().contains("not a socket"));
}
