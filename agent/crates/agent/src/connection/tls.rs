//! TLS for the agent channel and the application-level Core identity check.
//!
//! The agent always checks the Core twice: the TLS server certificate (in
//! `pinned` mode against the Core CA and the fixed Core name, in `proxy`
//! mode against the proxy's chain for the URL host), and the `welcome`
//! message's certificate plus proof against the Core CA. The second check is
//! what still holds when a proxy terminates TLS (T-024).

use crate::config::CoreTransport;
use anyhow::{Context, Result, anyhow, bail};
use hazelminx_protocol::CORE_SERVER_NAME;
use rustls::pki_types::pem::PemObject;
use rustls::pki_types::{CertificateDer, PrivateKeyDer, PrivatePkcs8KeyDer, ServerName, UnixTime};
use rustls::{ClientConfig, RootCertStore};
use std::path::{Path, PathBuf};
use std::sync::Arc;

pub const NODE_CERT_FILE: &str = "node.crt";
pub const CORE_CA_FILE: &str = "core-ca.crt";
/// A renewed certificate not yet proven in a handshake (certificate rotation).
pub const NEXT_CERT_FILE: &str = "node.crt.next";
/// The certificate a successful rotation replaced.
pub const PREV_CERT_FILE: &str = "node.crt.prev";
/// The privileged executor's grant issuer key handed over at enrollment;
/// the installer copies it to /etc/hazelminx/privileged/issuer.pub.
pub const ISSUER_KEY_FILE: &str = "privileged-issuer.pub";

/// Certificates and key the channel needs, read from `state_dir/identity`.
pub struct TrustMaterial {
    pub node_cert_pem: String,
    /// True when `node.crt.next` (a renewed certificate) is in use.
    pub using_next: bool,
    node_cert: CertificateDer<'static>,
    node_key: PrivatePkcs8KeyDer<'static>,
    core_ca: CertificateDer<'static>,
}

impl TrustMaterial {
    /// Reads `node.key`, `node.crt` and `core-ca.crt`. The last two are
    /// written by enrollment (ticket 019); until then the agent cannot
    /// connect and says so.
    pub fn load(identity_dir: &Path) -> Result<TrustMaterial> {
        let read = |name: &str| -> Result<(PathBuf, Vec<u8>)> {
            let p = identity_dir.join(name);
            let b = std::fs::read(&p).with_context(|| {
                format!(
                    "read {} (this node is not enrolled with a Core yet)",
                    p.display()
                )
            })?;
            Ok((p, b))
        };
        let using_next = identity_dir.join(NEXT_CERT_FILE).exists();
        let (cert_path, cert_pem) = read(if using_next {
            NEXT_CERT_FILE
        } else {
            NODE_CERT_FILE
        })?;
        let (ca_path, ca_pem) = read(CORE_CA_FILE)?;
        let (key_path, key_pem) = read(crate::identity::PRIVATE_KEY_FILE)?;
        let node_cert = single_cert(&cert_pem).with_context(|| cert_path.display().to_string())?;
        let core_ca = single_cert(&ca_pem).with_context(|| ca_path.display().to_string())?;
        let node_key = PrivatePkcs8KeyDer::from_pem_slice(&key_pem)
            .map_err(|_| anyhow!("{} is not a PKCS#8 private key", key_path.display()))?;
        Ok(TrustMaterial {
            node_cert_pem: String::from_utf8(cert_pem).context("node.crt is not UTF-8")?,
            using_next,
            node_cert,
            node_key,
            core_ca,
        })
    }

    /// TLS client configuration (TLS 1.3 only, ring provider). In proxy mode
    /// without `proxy_ca_file` the system trust store is used.
    pub fn client_config(
        &self,
        transport: CoreTransport,
        proxy_ca_file: Option<&Path>,
    ) -> Result<Arc<ClientConfig>> {
        let mut roots = RootCertStore::empty();
        let provider = Arc::new(rustls::crypto::ring::default_provider());
        let builder = ClientConfig::builder_with_provider(provider)
            .with_protocol_versions(&[&rustls::version::TLS13])
            .context("TLS 1.3 with the ring provider")?;
        let cfg = match transport {
            CoreTransport::Pinned => {
                roots
                    .add(self.core_ca.clone())
                    .context("core-ca.crt is not a usable trust anchor")?;
                builder
                    .with_root_certificates(roots)
                    .with_client_auth_cert(
                        vec![self.node_cert.clone()],
                        PrivateKeyDer::Pkcs8(self.node_key.clone_key()),
                    )
                    .context("node.crt and node.key do not form a usable client identity")?
            }
            CoreTransport::Proxy => {
                // The proxy terminates TLS, so a client certificate would only
                // reach the proxy; identity is proven in the handshake.
                builder
                    .with_root_certificates(proxy_roots(proxy_ca_file)?)
                    .with_no_client_auth()
            }
        };
        Ok(Arc::new(cfg))
    }

    /// Verifies the `welcome` certificate (chain to the Core CA, serverAuth,
    /// name `core.hazelminx.internal`, valid now) and its Ed25519 proof over
    /// `transcript`.
    pub fn verify_core(&self, core_cert_pem: &str, transcript: &[u8], proof: &[u8]) -> Result<()> {
        let der = single_cert(core_cert_pem.as_bytes()).context("welcome coreCertificate")?;
        let cert = webpki::EndEntityCert::try_from(&der)
            .map_err(|e| anyhow!("welcome coreCertificate does not parse: {e}"))?;
        let anchor = webpki::anchor_from_trusted_cert(&self.core_ca)
            .map_err(|e| anyhow!("core-ca.crt is not a trust anchor: {e}"))?;
        let algs = rustls::crypto::ring::default_provider().signature_verification_algorithms;
        cert.verify_for_usage(
            algs.all,
            &[anchor],
            &[],
            UnixTime::now(),
            webpki::KeyUsage::server_auth(),
            None,
            None,
        )
        .map_err(|e| anyhow!("Core certificate is not issued by this node's Core CA: {e}"))?;
        let name = ServerName::try_from(CORE_SERVER_NAME).expect("constant is a DNS name");
        cert.verify_is_valid_for_subject_name(&name)
            .map_err(|e| anyhow!("Core certificate is not for {CORE_SERVER_NAME}: {e}"))?;
        cert.verify_signature(webpki::ring::ED25519, transcript, proof)
            .map_err(|_| anyhow!("Core proof does not verify against its certificate"))?;
        Ok(())
    }
}

/// Trust roots for a TLS-terminating proxy: `proxy_ca_file`, or the
/// operating system's trust store as for any other TLS client on the host.
pub fn proxy_roots(proxy_ca_file: Option<&Path>) -> Result<RootCertStore> {
    let mut roots = RootCertStore::empty();
    match proxy_ca_file {
        Some(p) => {
            let pem = std::fs::read(p).with_context(|| format!("read {}", p.display()))?;
            for c in CertificateDer::pem_slice_iter(&pem) {
                roots
                    .add(c.with_context(|| format!("parse {}", p.display()))?)
                    .with_context(|| format!("{} has an unusable certificate", p.display()))?;
            }
            anyhow::ensure!(!roots.is_empty(), "{} has no certificate", p.display());
        }
        None => {
            let native = rustls_native_certs::load_native_certs();
            for c in native.certs {
                let _ = roots.add(c); // skip certificates rustls cannot use
            }
            anyhow::ensure!(
                !roots.is_empty(),
                "no usable system root certificates for the proxy; set proxy_ca_file"
            );
        }
    }
    Ok(roots)
}

/// TLS 1.3 client configuration without a client certificate, for
/// enrollment. `trusted` is the Core CA once known; `None` in pinned mode
/// accepts any server certificate (CA discovery, whose answer is
/// authenticated by the enrollment code instead).
pub fn enroll_client_config(
    transport: CoreTransport,
    proxy_ca_file: Option<&Path>,
    trusted: Option<&CertificateDer<'static>>,
) -> Result<Arc<ClientConfig>> {
    let provider = Arc::new(rustls::crypto::ring::default_provider());
    let builder = ClientConfig::builder_with_provider(provider.clone())
        .with_protocol_versions(&[&rustls::version::TLS13])
        .context("TLS 1.3 with the ring provider")?;
    let cfg = match (transport, trusted) {
        (CoreTransport::Proxy, _) => builder
            .with_root_certificates(proxy_roots(proxy_ca_file)?)
            .with_no_client_auth(),
        (CoreTransport::Pinned, Some(ca)) => {
            let mut roots = RootCertStore::empty();
            roots
                .add(ca.clone())
                .context("the Core CA is not a usable trust anchor")?;
            builder.with_root_certificates(roots).with_no_client_auth()
        }
        (CoreTransport::Pinned, None) => builder
            .dangerous()
            .with_custom_certificate_verifier(Arc::new(AnyServer(provider)))
            .with_no_client_auth(),
    };
    Ok(Arc::new(cfg))
}

/// Accepts any server certificate (still checking the handshake signature
/// against it). Only used to fetch the Core CA during enrollment; that
/// answer is checked with the enrollment code's MAC before anything is sent.
#[derive(Debug)]
struct AnyServer(Arc<rustls::crypto::CryptoProvider>);

impl rustls::client::danger::ServerCertVerifier for AnyServer {
    fn verify_server_cert(
        &self,
        _end_entity: &CertificateDer<'_>,
        _intermediates: &[CertificateDer<'_>],
        _server_name: &ServerName<'_>,
        _ocsp_response: &[u8],
        _now: UnixTime,
    ) -> std::result::Result<rustls::client::danger::ServerCertVerified, rustls::Error> {
        Ok(rustls::client::danger::ServerCertVerified::assertion())
    }

    fn verify_tls12_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> std::result::Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls12_signature(
            message,
            cert,
            dss,
            &self.0.signature_verification_algorithms,
        )
    }

    fn verify_tls13_signature(
        &self,
        message: &[u8],
        cert: &CertificateDer<'_>,
        dss: &rustls::DigitallySignedStruct,
    ) -> std::result::Result<rustls::client::danger::HandshakeSignatureValid, rustls::Error> {
        rustls::crypto::verify_tls13_signature(
            message,
            cert,
            dss,
            &self.0.signature_verification_algorithms,
        )
    }

    fn supported_verify_schemes(&self) -> Vec<rustls::SignatureScheme> {
        self.0.signature_verification_algorithms.supported_schemes()
    }
}

/// DER prefix of an Ed25519 SubjectPublicKeyInfo; the raw key follows.
const ED25519_SPKI_PREFIX: [u8; 12] = [
    0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00,
];

/// Checks a node certificate handed over by the Core: one PEM certificate,
/// issued by `ca` for client authentication, valid now, for `public_key`.
pub fn verify_node_certificate(
    pem: &str,
    ca: &CertificateDer<'static>,
    public_key: &[u8; 32],
) -> Result<()> {
    let der = single_cert(pem.as_bytes()).context("node certificate")?;
    let cert = webpki::EndEntityCert::try_from(&der)
        .map_err(|e| anyhow!("node certificate does not parse: {e}"))?;
    let anchor = webpki::anchor_from_trusted_cert(ca)
        .map_err(|e| anyhow!("the Core CA is not a trust anchor: {e}"))?;
    let algs = rustls::crypto::ring::default_provider().signature_verification_algorithms;
    cert.verify_for_usage(
        algs.all,
        &[anchor],
        &[],
        UnixTime::now(),
        webpki::KeyUsage::client_auth(),
        None,
        None,
    )
    .map_err(|e| anyhow!("node certificate is not issued by the Core CA for client use: {e}"))?;
    let mut spki = ED25519_SPKI_PREFIX.to_vec();
    spki.extend_from_slice(public_key);
    anyhow::ensure!(
        der.as_ref()
            .windows(spki.len())
            .any(|w| w == spki.as_slice()),
        "node certificate is for another key"
    );
    Ok(())
}

/// Parses one PEM certificate.
pub fn parse_certificate(pem: &str) -> Result<CertificateDer<'static>> {
    single_cert(pem.as_bytes())
}

/// Writes `bytes` to `dir/name` through a temporary file and a rename, so
/// a crash never leaves half a file.
pub fn write_atomic(dir: &Path, name: &str, bytes: &[u8]) -> Result<()> {
    use std::io::Write;
    let tmp = dir.join(format!(".{name}.tmp"));
    let mut opts = std::fs::OpenOptions::new();
    opts.write(true).create(true).truncate(true);
    #[cfg(unix)]
    {
        use std::os::unix::fs::OpenOptionsExt;
        opts.mode(0o600);
    }
    let mut f = opts
        .open(&tmp)
        .with_context(|| format!("create {}", tmp.display()))?;
    f.write_all(bytes)?;
    f.sync_all()?;
    drop(f);
    std::fs::rename(&tmp, dir.join(name))
        .with_context(|| format!("rename {} into place", tmp.display()))?;
    Ok(())
}

/// After a handshake succeeded with `node.crt.next`, makes it the
/// certificate and keeps the old one as `node.crt.prev`.
pub fn promote_next(dir: &Path) -> Result<()> {
    let cur = dir.join(NODE_CERT_FILE);
    if cur.exists() {
        std::fs::rename(&cur, dir.join(PREV_CERT_FILE)).context("keep the previous certificate")?;
    }
    std::fs::rename(dir.join(NEXT_CERT_FILE), &cur).context("switch to the renewed certificate")
}

/// Drops a renewed certificate that did not work; the current one is used
/// from the next connection on.
pub fn discard_next(dir: &Path) -> Result<()> {
    match std::fs::remove_file(dir.join(NEXT_CERT_FILE)) {
        Err(e) if e.kind() != std::io::ErrorKind::NotFound => {
            Err(e).context("remove node.crt.next")
        }
        _ => Ok(()),
    }
}

/// The TLS server name to verify: the fixed Core name when pinned (so the
/// Core's IP or hostname may change, T-018), the URL host behind a proxy.
pub fn server_name(transport: CoreTransport, host: &str) -> Result<ServerName<'static>> {
    let name = match transport {
        CoreTransport::Pinned => CORE_SERVER_NAME.to_string(),
        CoreTransport::Proxy => host.to_string(),
    };
    ServerName::try_from(name).map_err(|e| anyhow!("TLS server name {host:?}: {e}"))
}

fn single_cert(pem: &[u8]) -> Result<CertificateDer<'static>> {
    let mut it = CertificateDer::pem_slice_iter(pem);
    let first = match it.next() {
        Some(c) => c.map_err(|e| anyhow!("bad PEM certificate: {e:?}"))?,
        None => bail!("no PEM certificate"),
    };
    if it.next().is_some() {
        bail!("expected exactly one certificate");
    }
    Ok(first)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::path::PathBuf;

    fn fixtures() -> PathBuf {
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../engineering/contracts/fixtures")
    }

    /// Lays out an identity dir from the contract fixtures.
    pub fn fixture_identity(dir: &Path, node: &str) {
        let pki = fixtures().join("pki");
        std::fs::create_dir_all(dir).unwrap();
        std::fs::copy(pki.join(format!("{node}.key")), dir.join("node.key")).unwrap();
        std::fs::copy(pki.join(format!("{node}.crt")), dir.join(NODE_CERT_FILE)).unwrap();
        std::fs::copy(pki.join("ca.crt"), dir.join(CORE_CA_FILE)).unwrap();
    }

    fn signature() -> serde_json::Value {
        serde_json::from_str(&std::fs::read_to_string(fixtures().join("signature.json")).unwrap())
            .unwrap()
    }

    fn b64(s: &str) -> Vec<u8> {
        use base64::Engine;
        base64::engine::general_purpose::STANDARD.decode(s).unwrap()
    }

    #[test]
    fn fixture_core_proof_verifies_and_tampering_fails() {
        let dir = tempfile::tempdir().unwrap();
        fixture_identity(dir.path(), "node");
        let m = TrustMaterial::load(dir.path()).unwrap();
        let sig = signature();
        let core_pem = std::fs::read_to_string(fixtures().join("pki/core.crt")).unwrap();
        let t = sig["coreTranscript"].as_str().unwrap().as_bytes();
        let proof = b64(sig["coreProof"].as_str().unwrap());
        m.verify_core(&core_pem, t, &proof).unwrap();
        // The agent transcript (role "agent") must not pass as a Core proof.
        let at = sig["agentTranscript"].as_str().unwrap().as_bytes();
        assert!(m.verify_core(&core_pem, at, &proof).is_err());
        // A node certificate is not a Core certificate (clientAuth only).
        let node_pem = std::fs::read_to_string(fixtures().join("pki/node.crt")).unwrap();
        let err = m.verify_core(&node_pem, t, &proof).unwrap_err();
        assert!(
            err.to_string().contains("not issued") || err.to_string().contains("not for"),
            "{err:#}"
        );
    }

    #[test]
    fn core_certificate_from_another_ca_is_refused() {
        let dir = tempfile::tempdir().unwrap();
        fixture_identity(dir.path(), "node");
        // Pin the rogue CA instead: the real Core certificate no longer chains.
        std::fs::copy(
            fixtures().join("pki/rogue-ca.crt"),
            dir.path().join(CORE_CA_FILE),
        )
        .unwrap();
        let m = TrustMaterial::load(dir.path()).unwrap();
        let sig = signature();
        let core_pem = std::fs::read_to_string(fixtures().join("pki/core.crt")).unwrap();
        let err = m
            .verify_core(
                &core_pem,
                sig["coreTranscript"].as_str().unwrap().as_bytes(),
                &b64(sig["coreProof"].as_str().unwrap()),
            )
            .unwrap_err();
        assert!(err.to_string().contains("not issued"), "{err:#}");
    }

    #[test]
    fn missing_enrollment_files_say_so() {
        let dir = tempfile::tempdir().unwrap();
        let err = TrustMaterial::load(dir.path()).err().unwrap();
        assert!(format!("{err:#}").contains("not enrolled"), "{err:#}");
    }

    fn node_public_key(name: &str) -> [u8; 32] {
        use ed25519_dalek::pkcs8::DecodePrivateKey;
        let pem = std::fs::read_to_string(fixtures().join(format!("pki/{name}.key"))).unwrap();
        ed25519_dalek::SigningKey::from_pkcs8_pem(&pem)
            .unwrap()
            .verifying_key()
            .to_bytes()
    }

    #[test]
    fn node_certificate_must_chain_to_the_ca_and_carry_our_key() {
        let ca = single_cert(&std::fs::read(fixtures().join("pki/ca.crt")).unwrap()).unwrap();
        let pem = |n: &str| std::fs::read_to_string(fixtures().join(format!("pki/{n}"))).unwrap();
        let key = node_public_key("node");
        verify_node_certificate(&pem("node.crt"), &ca, &key).unwrap();
        let err = verify_node_certificate(&pem("other-node.crt"), &ca, &key).unwrap_err();
        assert!(err.to_string().contains("another key"), "{err:#}");
        assert!(verify_node_certificate(&pem("node-rogue.crt"), &ca, &key).is_err());
        assert!(verify_node_certificate(&pem("core.crt"), &ca, &key).is_err());
        assert!(verify_node_certificate("garbage", &ca, &key).is_err());
    }

    #[test]
    fn renewed_certificate_is_tried_then_promoted_or_discarded() {
        let dir = tempfile::tempdir().unwrap();
        fixture_identity(dir.path(), "node");
        let current = std::fs::read_to_string(dir.path().join(NODE_CERT_FILE)).unwrap();
        let next = std::fs::read_to_string(fixtures().join("pki/node-expired.crt")).unwrap();
        write_atomic(dir.path(), NEXT_CERT_FILE, next.as_bytes()).unwrap();
        let m = TrustMaterial::load(dir.path()).unwrap();
        assert!(m.using_next);
        assert_eq!(m.node_cert_pem, next);
        discard_next(dir.path()).unwrap();
        let m = TrustMaterial::load(dir.path()).unwrap();
        assert!(!m.using_next);
        assert_eq!(m.node_cert_pem, current);
        discard_next(dir.path()).unwrap(); // nothing to remove is fine
        write_atomic(dir.path(), NEXT_CERT_FILE, next.as_bytes()).unwrap();
        promote_next(dir.path()).unwrap();
        assert_eq!(
            std::fs::read_to_string(dir.path().join(NODE_CERT_FILE)).unwrap(),
            next
        );
        assert_eq!(
            std::fs::read_to_string(dir.path().join(PREV_CERT_FILE)).unwrap(),
            current
        );
        assert!(!dir.path().join(NEXT_CERT_FILE).exists());
    }

    #[test]
    fn pinned_server_name_ignores_the_url_host() {
        let n = server_name(CoreTransport::Pinned, "10.0.0.7").unwrap();
        assert_eq!(n, ServerName::try_from(CORE_SERVER_NAME).unwrap());
        let n = server_name(CoreTransport::Proxy, "core.example.com").unwrap();
        assert_eq!(n, ServerName::try_from("core.example.com").unwrap());
    }

    #[test]
    fn client_configs_build_for_both_transports() {
        let dir = tempfile::tempdir().unwrap();
        fixture_identity(dir.path(), "node");
        let m = TrustMaterial::load(dir.path()).unwrap();
        m.client_config(CoreTransport::Pinned, None).unwrap();
        m.client_config(CoreTransport::Proxy, None).unwrap();
        m.client_config(CoreTransport::Proxy, Some(&fixtures().join("pki/ca.crt")))
            .unwrap();
    }
}
