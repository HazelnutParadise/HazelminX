package composedeploy

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"github.com/HazelnutParadise/hazelminx/core/internal/grants"
	"github.com/HazelnutParadise/hazelminx/core/internal/protocol"
)

type contractFixture struct {
	Manifest       Manifest               `json:"manifest"`
	Staged         []StagedFile           `json:"staged"`
	DigestText     string                 `json:"digestText"`
	Digest         string                 `json:"digest"`
	BindingCheck   string                 `json:"bindingCheck"`
	BindingDeploy  string                 `json:"bindingDeploy"`
	Grant          protocol.ExecutorGrant `json:"grant"`
	GrantCanonical string                 `json:"grantCanonical"`
}

// The executor checks the same fixture (agent/crates/privileged/tests/compose_contract.rs).
func TestManifestMatchesContractFixture(t *testing.T) {
	b, err := os.ReadFile("../../../engineering/contracts/fixtures/compose-manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var f contractFixture
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	byHash := map[string]string{}
	for _, s := range f.Staged {
		byHash[s.SHA256] = s.Content
	}
	for _, mf := range f.Manifest.Files {
		files[mf.Path] = []byte(byHash[mf.SHA256])
	}
	m, _ := NewManifest(f.Manifest.Project, f.Manifest.Revision, f.Manifest.Environment, f.Manifest.MainFile,
		f.Manifest.ExtraFiles, f.Manifest.Profiles, "", files)
	if m.DigestText() != f.DigestText || m.Digest() != f.Digest {
		t.Fatalf("digest text\n%q\nwant\n%q", m.DigestText(), f.DigestText)
	}
	if m.Binding("compose_check") != f.BindingCheck || m.Binding("compose_deploy") != f.BindingDeploy {
		t.Fatal("bindings differ from the fixture")
	}
	seed, _ := hex.DecodeString("0707070707070707070707070707070707070707070707070707070707070707")
	iss := grants.New(ed25519.NewKeyFromSeed(seed), nil)
	g := f.Grant
	if string(grants.Canonical(g)) != f.GrantCanonical {
		t.Fatalf("canonical %q", grants.Canonical(g))
	}
	g.Signature = ""
	iss.Sign(&g)
	if g.Signature != f.Grant.Signature {
		t.Fatal("signature differs from the fixture")
	}
}

func TestManifestSortsFilesAndStagesEachContentOnce(t *testing.T) {
	m, staged := NewManifest("p", "r", "env:/run/docker.sock", "b.yml", nil, nil, "app",
		map[string][]byte{"b.yml": []byte("x"), "a.yml": []byte("x")})
	if m.Files[0].Path != "a.yml" || len(staged) != 1 || m.WorkingDir == nil || *m.WorkingDir != "app" {
		t.Fatalf("%+v %+v", m, staged)
	}
	if m.Binding("compose_check") == m.Binding("compose_deploy") {
		t.Fatal("a check grant must not authorize a deploy")
	}
}
