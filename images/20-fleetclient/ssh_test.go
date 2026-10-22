package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

const (
	keyA = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIOiuTuZpapVCjAUUGcXj2zMLBbrr0QyMsca1UEx+EBWQ alice@example.com"
	keyB = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIL85YGKsHVZg1FilB2p/FQs777cdPnqWtr8rfoKImcUU bob@example.com"
	own  = "ssh-rsa AAAAB3NzaC1yc2E own@laptop"
)

func TestUpdateAuthorizedKeys(t *testing.T) {
	got := updateAuthorizedKeys(own+"\n", []string{keyA, keyB, keyA})
	want := own + "\n" + keysBegin + "\n" + keyA + "\n" + keyB + "\n" + keysEnd + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if !slices.Equal(managedKeys(got), []string{keyA, keyB}) {
		t.Fatalf("managed keys: %q", managedKeys(got))
	}
	// Replacing keeps own keys, also ones added after the block.
	got = updateAuthorizedKeys(got+"ssh-rsa AAAA later\n", []string{keyB})
	want = own + "\nssh-rsa AAAA later\n" + keysBegin + "\n" + keyB + "\n" + keysEnd + "\n"
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got = updateAuthorizedKeys(got, []string{}); got != own+"\nssh-rsa AAAA later\n" {
		t.Fatalf("empty list should remove the block: %q", got)
	}
	if got = updateAuthorizedKeys("", nil); got != "" {
		t.Fatalf("%q", got)
	}
}

func TestApplyKeys(t *testing.T) {
	c := &client{homeDir: t.TempDir()}
	os.MkdirAll(filepath.Join(c.homeDir, "rancher"), 0755)
	os.MkdirAll(filepath.Join(c.homeDir, "docker", ".ssh"), 0700)
	os.WriteFile(filepath.Join(c.homeDir, "docker", ".ssh", "authorized_keys"), []byte(own+"\n"), 0600)

	keys, err := c.applyKeys([]string{keyA})
	if err != nil || !slices.Equal(keys, []string{keyA}) {
		t.Fatalf("%q %v", keys, err)
	}
	for _, u := range []string{"rancher", "docker"} {
		file := filepath.Join(c.homeDir, u, ".ssh", "authorized_keys")
		fi, err := os.Stat(file)
		if err != nil || fi.Mode().Perm() != 0600 {
			t.Fatalf("%s: %v %v", u, fi, err)
		}
	}
	data, _ := os.ReadFile(filepath.Join(c.homeDir, "docker", ".ssh", "authorized_keys"))
	if string(data) != own+"\n"+keysBegin+"\n"+keyA+"\n"+keysEnd+"\n" {
		t.Fatalf("own keys must be kept:\n%s", data)
	}
	if keys, _ = c.applyKeys([]string{}); keys == nil || len(keys) != 0 {
		t.Fatalf("expected empty list, got %#v", keys)
	}
}
