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

/// Certificates and key the channel needs, read from `state_dir/identity`.
pub struct TrustMaterial {
    pub node_cert_pem: String,
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
        let (cert_path, cert_pem) = read(NODE_CERT_FILE)?;
        let (ca_path, ca_pem) = read(CORE_CA_FILE)?;
        let (key_path, key_pem) = read(crate::identity::PRIVATE_KEY_FILE)?;
        let node_cert = single_cert(&cert_pem).with_context(|| cert_path.display().to_string())?;
        let core_ca = single_cert(&ca_pem).with_context(|| ca_path.display().to_string())?;
        let node_key = PrivatePkcs8KeyDer::from_pem_slice(&key_pem)
            .map_err(|_| anyhow!("{} is not a PKCS#8 private key", key_path.display()))?;
        Ok(TrustMaterial {
            node_cert_pem: String::from_utf8(cert_pem).context("node.crt is not UTF-8")?,
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
                match proxy_ca_file {
                    Some(p) => {
                        let pem =
                            std::fs::read(p).with_context(|| format!("read {}", p.display()))?;
                        for c in CertificateDer::pem_slice_iter(&pem) {
                            roots
                                .add(c.with_context(|| format!("parse {}", p.display()))?)
                                .with_context(|| {
                                    format!("{} has an unusable certificate", p.display())
                                })?;
                        }
                        anyhow::ensure!(!roots.is_empty(), "{} has no certificate", p.display());
                    }
                    None => {
                        // The operating system's trust store, as for any
                        // other TLS client on the host.
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
                // The proxy terminates TLS, so a client certificate would only
                // reach the proxy; identity is proven in the handshake.
                builder.with_root_certificates(roots).with_no_client_auth()
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
        PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../../docs/contracts/fixtures")
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
