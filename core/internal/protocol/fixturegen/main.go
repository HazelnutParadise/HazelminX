//go:build ignore

// fixturegen regenerates the deterministic parts of engineering/contracts/fixtures:
// the test PKI (pki/*.pem), signature.json and the handshake samples that
// embed certificates and proofs. Every key comes from a fixed seed and
// Ed25519 signatures are deterministic, so running it twice gives identical
// files. The keys are public test material and must never be deployed.
//
//	go run ./internal/protocol/fixturegen/main.go ../engineering/contracts/fixtures
package main

import (
	"crypto/ecdh"
	"crypto/ed25519"
	"crypto/hkdf"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"os"
	"path/filepath"
	"time"
)

const coreServerName = "core.hazelminx.internal"

func seedKey(label string) ed25519.PrivateKey {
	s := sha256.Sum256([]byte("hazelminx-fixture-" + label))
	return ed25519.NewKeyFromSeed(s[:])
}

func fingerprint(pub ed25519.PublicKey) string {
	s := sha256.Sum256(pub)
	return hex.EncodeToString(s[:])
}

func certPEM(der []byte) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func keyPEM(k ed25519.PrivateKey) string {
	der, err := x509.MarshalPKCS8PrivateKey(k)
	must(err)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func must(err error) {
	if err != nil {
		panic(err)
	}
}

var (
	notBefore = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	notAfter  = time.Date(2126, 1, 1, 0, 0, 0, 0, time.UTC)
)

func makeCA(label string, serial int64) (*x509.Certificate, ed25519.PrivateKey) {
	k := seedKey(label)
	tpl := &x509.Certificate{
		SerialNumber:          big.NewInt(serial),
		Subject:               pkix.Name{CommonName: "HazelminX fixture CA " + label},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(nil, tpl, tpl, k.Public(), k)
	must(err)
	c, err := x509.ParseCertificate(der)
	must(err)
	return c, k
}

func issue(ca *x509.Certificate, caKey ed25519.PrivateKey, pub ed25519.PublicKey, serial int64, server bool, nb, na time.Time) []byte {
	tpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		NotBefore:    nb,
		NotAfter:     na,
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		tpl.Subject = pkix.Name{CommonName: coreServerName}
		tpl.DNSNames = []string{coreServerName}
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
	} else {
		tpl.Subject = pkix.Name{CommonName: fingerprint(pub)}
		tpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(nil, tpl, ca, pub, caKey)
	must(err)
	return der
}

func transcript(role, agentNonce, coreNonce, agentKex, coreKex string, version int, node, session, helloDigest, welcomeDigest string) string {
	return fmt.Sprintf("hazelminx-session-v1\n%s\n%s\n%s\n%s\n%s\n%d\n%s\n%s\n%s\n%s\n", role, agentNonce, coreNonce, agentKex, coreKex, version, node, session, helloDigest, welcomeDigest)
}

// welcomeDigest follows the length-prefixed encoding of the contract.
func welcomeDigest(coreVersion string, heartbeat int, caps []map[string]any) string {
	var b []byte
	u32 := func(v int) { b = binary.BigEndian.AppendUint32(b, uint32(v)) }
	lp := func(x string) { u32(len(x)); b = append(b, x...) }
	lp("hazelminx-welcome-v1")
	lp(coreVersion)
	u32(heartbeat)
	u32(len(caps))
	for _, c := range caps {
		lp(c["name"].(string))
		u32(c["version"].(int))
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func kexKey(label string) *ecdh.PrivateKey {
	s := sha256.Sum256([]byte("hazelminx-fixture-kex-" + label))
	k, err := ecdh.X25519().NewPrivateKey(s[:])
	must(err)
	return k
}

func frameMAC(key []byte, n uint64, body string) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("hazelminx-frame-v1\n"))
	var nb [8]byte
	binary.BigEndian.PutUint64(nb[:], n)
	m.Write(nb[:])
	m.Write([]byte(body))
	return base64.StdEncoding.EncodeToString(m.Sum(nil))
}

func writeFile(dir, name, body string) {
	p := filepath.Join(dir, name)
	must(os.MkdirAll(filepath.Dir(p), 0o755))
	must(os.WriteFile(p, []byte(body), 0o644))
}

func writeJSON(dir, name string, v any) {
	b, err := json.MarshalIndent(v, "", "  ")
	must(err)
	writeFile(dir, name, string(b)+"\n")
}

func nonce(label string) string {
	s := sha256.Sum256([]byte("hazelminx-fixture-nonce-" + label))
	return base64.RawURLEncoding.EncodeToString(s[:])
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./internal/protocol/fixturegen/main.go <fixtures-dir>")
		os.Exit(2)
	}
	dir := os.Args[1]

	ca, caKey := makeCA("ca", 1)
	rogueCA, rogueKey := makeCA("rogue-ca", 2)
	coreKey := seedKey("core")
	nodeKey := seedKey("node")
	otherKey := seedKey("other-node")

	coreDER := issue(ca, caKey, coreKey.Public().(ed25519.PublicKey), 10, true, notBefore, notAfter)
	nodeDER := issue(ca, caKey, nodeKey.Public().(ed25519.PublicKey), 20, false, notBefore, notAfter)
	otherDER := issue(ca, caKey, otherKey.Public().(ed25519.PublicKey), 21, false, notBefore, notAfter)
	expiredDER := issue(ca, caKey, nodeKey.Public().(ed25519.PublicKey), 22, false, notBefore, time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC))
	rogueNodeDER := issue(rogueCA, rogueKey, nodeKey.Public().(ed25519.PublicKey), 30, false, notBefore, notAfter)

	pki := filepath.Join(dir, "pki")
	writeFile(pki, "ca.crt", certPEM(ca.Raw))
	writeFile(pki, "core.crt", certPEM(coreDER))
	writeFile(pki, "core.key", keyPEM(coreKey))
	writeFile(pki, "node.crt", certPEM(nodeDER))
	writeFile(pki, "node.key", keyPEM(nodeKey))
	writeFile(pki, "other-node.crt", certPEM(otherDER))
	writeFile(pki, "other-node.key", keyPEM(otherKey))
	writeFile(pki, "node-expired.crt", certPEM(expiredDER))
	writeFile(pki, "rogue-ca.crt", certPEM(rogueCA.Raw))
	writeFile(pki, "node-rogue.crt", certPEM(rogueNodeDER))

	nodeFP := fingerprint(nodeKey.Public().(ed25519.PublicKey))
	agentNonce, coreNonce := nonce("agent"), nonce("core")
	const version = 1
	const session = "ses_fixture0001"
	agentKexKey, coreKexKey := kexKey("agent"), kexKey("core")
	agentKex := base64.RawURLEncoding.EncodeToString(agentKexKey.PublicKey().Bytes())
	coreKex := base64.RawURLEncoding.EncodeToString(coreKexKey.PublicKey().Bytes())
	shared, err := agentKexKey.ECDH(coreKexKey.PublicKey())
	must(err)
	c2a, err := hkdf.Key(sha256.New, shared, nil, "hazelminx-session-v1 core->agent "+session, 32)
	must(err)
	a2c, err := hkdf.Key(sha256.New, shared, nil, "hazelminx-session-v1 agent->core "+session, 32)
	must(err)
	caps := []map[string]any{{"name": "jobs", "version": 1}, {"name": "agent.info", "version": 1}}
	hello := map[string]any{
		"type":            "hello",
		"protocol":        map[string]any{"min": 1, "max": 1},
		"agentVersion":    "0.1.0",
		"nodeCertificate": certPEM(nodeDER),
		"nonce":           agentNonce,
		"kex":             agentKex,
		"capabilities":    caps,
	}
	helloText, err := json.Marshal(hello)
	must(err)
	helloSum := sha256.Sum256(helloText)
	helloDigest := hex.EncodeToString(helloSum[:])
	coreCaps := []map[string]any{{"name": "jobs", "version": 1}}
	const coreVersion, heartbeat = "0.1.0", 10
	wDigest := welcomeDigest(coreVersion, heartbeat, coreCaps)
	coreT := transcript("core", agentNonce, coreNonce, agentKex, coreKex, version, nodeFP, session, helloDigest, wDigest)
	agentT := transcript("agent", agentNonce, coreNonce, agentKex, coreKex, version, nodeFP, session, helloDigest, wDigest)
	coreProof := base64.StdEncoding.EncodeToString(ed25519.Sign(coreKey, []byte(coreT)))
	agentProof := base64.StdEncoding.EncodeToString(ed25519.Sign(nodeKey, []byte(agentT)))

	writeJSON(dir, "signature.json", map[string]any{
		"note":              "Test-only keys derived from public seeds. Never deploy.",
		"nodeSeedHex":       hex.EncodeToString(nodeKey.Seed()),
		"nodePublicKeyHex":  hex.EncodeToString(nodeKey.Public().(ed25519.PublicKey)),
		"nodeFingerprint":   nodeFP,
		"coreSeedHex":       hex.EncodeToString(coreKey.Seed()),
		"agentNonce":        agentNonce,
		"coreNonce":         coreNonce,
		"agentKexSeedHex":   hex.EncodeToString(agentKexKey.Bytes()),
		"coreKexSeedHex":    hex.EncodeToString(coreKexKey.Bytes()),
		"agentKex":          agentKex,
		"coreKex":           coreKex,
		"sharedSecretHex":   hex.EncodeToString(shared),
		"keyCoreToAgentHex": hex.EncodeToString(c2a),
		"keyAgentToCoreHex": hex.EncodeToString(a2c),
		"protocolVersion":   version,
		"sessionId":         session,
		"helloText":         string(helloText),
		"helloDigest":       helloDigest,
		"welcomeDigest":     wDigest,
		"coreTranscript":    coreT,
		"agentTranscript":   agentT,
		"coreProof":         coreProof,
		"agentProof":        agentProof,
	})

	msgs := filepath.Join(dir, "messages")
	writeJSON(msgs, "hello.json", hello)
	writeJSON(msgs, "welcome.json", map[string]any{
		"type":             "welcome",
		"protocolVersion":  version,
		"coreVersion":      coreVersion,
		"coreCertificate":  certPEM(coreDER),
		"nonce":            coreNonce,
		"kex":              coreKex,
		"sessionId":        session,
		"proof":            coreProof,
		"heartbeatSeconds": heartbeat,
		"capabilities":     coreCaps,
	})
	writeJSON(msgs, "auth.json", map[string]any{"type": "auth", "proof": agentProof})

	// Two frames per direction: the first message after ready each way.
	ackBody := `{"type":"ack","seq":17}`
	hbBody := `{"type":"heartbeat","at":"2026-11-08T03:04:05Z"}`
	writeJSON(dir, "frames.json", map[string]any{
		"note":              "Frames sealed with the session keys of signature.json. n counts frames per direction from 1.",
		"keyCoreToAgentHex": hex.EncodeToString(c2a),
		"keyAgentToCoreHex": hex.EncodeToString(a2c),
		"coreToAgent": []map[string]any{
			{"n": 1, "body": ackBody, "mac": frameMAC(c2a, 1, ackBody)},
			{"n": 2, "body": hbBody, "mac": frameMAC(c2a, 2, hbBody)},
		},
		"agentToCore": []map[string]any{
			{"n": 1, "body": hbBody, "mac": frameMAC(a2c, 1, hbBody)},
		},
	})
}
