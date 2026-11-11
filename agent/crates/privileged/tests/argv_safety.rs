//! T-039 / SEC-006 at the process boundary: hostile parameter values reach
//! the child as one argv entry or one environment value and are never
//! interpreted by a shell. Uses the real runner and real binaries that exist
//! on every Linux and macOS host (`printf`, `printenv`, `ls`).

use hazelminx_privileged::command::{
    CommandRunner, CommandSpec, HostRunner, OutputSink, StreamKind,
};
use hazelminx_privileged::params::{EnvVar, HostPath};
use std::path::Path;

const HOSTILE: &str = "it's \"quoted\" name; rm -rf / && touch PWNED | cat\n$(touch PWNED2) `touch PWNED3` > out <in *";
/// Same, minus '/' (not a legal file-name byte): the T-039 file name.
const HOSTILE_NAME: &str = "it's \"quoted\" name; rm -rf ~ && touch PWNED | cat\n$(touch PWNED2) `touch PWNED3` > out <in *";

fn first_existing(cands: &[&str]) -> &'static str {
    for c in cands {
        if Path::new(c).exists() {
            return Box::leak(c.to_string().into_boxed_str());
        }
    }
    panic!("none of {cands:?} exists");
}

#[tokio::test]
async fn path_argument_with_shell_syntax_is_one_argv_entry() {
    let dir = tempfile::tempdir().unwrap();
    let full = dir.path().join(HOSTILE_NAME);
    std::fs::write(&full, b"data").unwrap();
    let path = HostPath::try_from(full.to_string_lossy().into_owned()).unwrap();

    let runner = HostRunner;
    let spec = CommandSpec::new(first_existing(&["/bin/ls", "/usr/bin/ls"])).arg(path.as_path());
    let out = runner.run(spec, OutputSink::discard()).await.unwrap();
    assert!(out.success(), "{}", out.stderr_lossy());
    assert_eq!(
        out.stdout_lossy().trim_end_matches('\n'),
        full.to_string_lossy()
    );

    // The sentinel files a shell would have created do not exist.
    for s in ["PWNED", "PWNED2", "PWNED3", "out"] {
        assert!(
            !dir.path().join(s).exists(),
            "{s} was created: shell interpreted the argument"
        );
        assert!(!Path::new(s).exists(), "{s} was created in cwd");
    }
}

#[tokio::test]
async fn printf_echoes_the_argument_byte_for_byte() {
    let runner = HostRunner;
    let spec = CommandSpec::new(first_existing(&["/usr/bin/printf", "/bin/printf"]))
        .arg("%s")
        .arg(HOSTILE);
    let out = runner.run(spec, OutputSink::discard()).await.unwrap();
    assert!(out.success());
    assert_eq!(out.stdout_lossy(), HOSTILE);
}

#[tokio::test]
async fn environment_values_are_passed_verbatim_and_nothing_else_leaks() {
    let runner = HostRunner;
    let var = EnvVar::new("HX_MSG", HOSTILE).unwrap();
    let spec = CommandSpec::new(first_existing(&["/usr/bin/printenv", "/bin/printenv"]))
        .arg("HX_MSG")
        .env(var.name(), var.value());
    let out = runner.run(spec, OutputSink::discard()).await.unwrap();
    assert!(out.success());
    assert_eq!(out.stdout_lossy(), format!("{HOSTILE}\n"));

    // The runner clears the inherited environment: only PATH/LANG and the spec's pairs exist.
    let spec = CommandSpec::new(first_existing(&["/usr/bin/printenv", "/bin/printenv"]));
    let out = runner.run(spec, OutputSink::discard()).await.unwrap();
    let names: Vec<&str> = out
        .stdout
        .split(|b| *b == b'\n')
        .filter(|l| !l.is_empty())
        .map(|l| std::str::from_utf8(l).unwrap().split('=').next().unwrap())
        .collect();
    assert!(
        names.iter().all(|n| ["PATH", "LANG", "LC_ALL"].contains(n)),
        "{names:?}"
    );
}

#[tokio::test]
async fn output_is_streamed_in_chunks_and_capped_in_capture() {
    // 6 MiB of zeros from a real process.
    let runner = HostRunner;
    let spec = CommandSpec::new(first_existing(&["/usr/bin/head", "/bin/head"]))
        .arg("-c")
        .arg((6 * 1024 * 1024).to_string())
        .arg("/dev/zero");
    let (sink, mut rx) = OutputSink::channel();
    let drain = tokio::spawn(async move {
        let mut n = 0usize;
        while let Some(c) = rx.recv().await {
            assert_eq!(c.stream, StreamKind::Stdout);
            n += c.data.len();
        }
        n
    });
    let out = runner.run(spec, sink).await.unwrap();
    assert!(out.success());
    assert!(out.truncated);
    assert_eq!(
        out.stdout.len(),
        hazelminx_privileged::command::CAPTURE_LIMIT
    );
    assert_eq!(drain.await.unwrap(), 6 * 1024 * 1024);
}
