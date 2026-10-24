package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// SSH is the SSH part of the desired state and of the report. A nil field
// is not managed.
type SSH struct {
	AuthorizedKeys         []string `json:"authorized_keys"`
	PasswordAuthentication *bool    `json:"password_authentication"`
	// DisableAfterBoot (Go duration) stops sshd once the server has been
	// up this long and no SSH session is active. "0s" enables it again.
	DisableAfterBoot string `json:"disable_after_boot,omitempty"`
	// Disabled is reported back: sshd is disabled.
	Disabled bool `json:"disabled,omitempty"`
}

const (
	keysBegin = "# BEGIN burmilla-fleet-manager"
	keysEnd   = "# END burmilla-fleet-manager"
)

// sshUsers get the managed keys, same users as cloud-config ssh_authorized_keys.
var sshUsers = []string{"rancher", "docker"}

// sshdScript runs in the console container. $1 is yes, no or empty (not
// managed). It writes a drop-in which wins over sshd_config (the console
// includes sshd_config.d first), reloads sshd and prints the effective
// setting.
const sshdScript = `set -e
f=/etc/ssh/sshd_config.d/50-burmilla-fleet.conf
if [ -n "$1" ]; then
  want="# Managed by fleet-client
PasswordAuthentication $1"
  if [ "$1" = no ]; then
    want="$want
KbdInteractiveAuthentication no"
  fi
  if [ "$(cat $f 2>/dev/null)" != "$want" ]; then
    mkdir -p /etc/ssh/sshd_config.d
    printf '%s\n' "$want" > $f.tmp
    mv $f.tmp $f
    sshd -t || { rm -f $f; exit 1; }
    if [ -s /run/sshd.pid ]; then kill -HUP "$(cat /run/sshd.pid)" 2>/dev/null || true; fi
  fi
fi
sshd -T 2>/dev/null | grep -i '^passwordauthentication '
`

// sshGate stops or starts sshd of the console for disable_after_boot
// (ros console sshd, BurmillaOS with sshd disable support). Unset is not
// managed, 0 enables sshd. It returns whether sshd is disabled and, while
// it waits for the deadline or for open sessions to end, how soon to
// check again.
func (c *client) sshGate(after string, boot, now time.Time) (bool, time.Duration, error) {
	status := sshdStatus()
	if after == "" {
		return status == "disabled", 0, nil
	}
	d, _ := time.ParseDuration(after)
	if d == 0 {
		if status == "disabled" {
			if _, err := capture(rosBin, "console", "sshd", "enable"); err != nil {
				return true, 0, fmt.Errorf("enable sshd: %v", err)
			}
			log.Print("sshd enabled")
		}
		return false, 0, nil
	}
	if boot.IsZero() {
		return status == "disabled", 0, nil
	}
	if left := boot.Add(d).Sub(now); left > 0 {
		return status == "disabled", max(left, 10*time.Second), nil
	}
	switch status {
	case "disabled":
		return true, 0, nil
	case "enabled":
	default:
		return false, 0, fmt.Errorf("this BurmillaOS version cannot disable sshd (ros console sshd)")
	}
	out, err := capture(rosBin, "console", "sshd", "disable", "--if-idle")
	if err != nil {
		return false, 0, fmt.Errorf("disable sshd: %v", err)
	}
	if sshdStatus() != "disabled" {
		// An SSH session is open, try again when it may have ended.
		log.Printf("sshd: %s", out)
		return false, time.Minute, nil
	}
	log.Printf("sshd disabled %s after boot", d)
	return true, 0, nil
}

// sshdStatus returns enabled, disabled or "" when not supported.
func sshdStatus() string {
	out, err := capture(rosBin, "console", "sshd", "status")
	if err != nil {
		return ""
	}
	switch out {
	case "enabled", "disabled":
		return out
	}
	return ""
}

// consoleAccessStatus returns whether sshd and the tty logins of the
// console are enabled (ros console access status), nil when this
// BurmillaOS version does not support it.
func consoleAccessStatus() *bool {
	out, err := capture(rosBin, "console", "access", "status")
	if err != nil {
		return nil
	}
	switch out {
	case "enabled", "disabled":
		enabled := out == "enabled"
		return &enabled
	}
	return nil
}

// applyConsoleAccess enables or disables sshd and the tty logins of the
// console. nil is not managed. It returns the state after applying, nil
// when the BurmillaOS version cannot report it.
func applyConsoleAccess(want *bool) (*bool, error) {
	status := consoleAccessStatus()
	if want == nil || (status != nil && *status == *want) {
		return status, nil
	}
	if status == nil {
		return nil, fmt.Errorf("this BurmillaOS version cannot manage console access (ros console access)")
	}
	action := "disable"
	if *want {
		action = "enable"
	}
	if _, err := capture(rosBin, "console", "access", action); err != nil {
		return status, fmt.Errorf("%s console access: %v", action, err)
	}
	log.Printf("console access %sd", action)
	status = consoleAccessStatus()
	if status == nil || *status != *want {
		return status, fmt.Errorf("console access not %sd", action)
	}
	return status, nil
}

// applySSH applies the desired SSH settings and returns the resulting
// state for the report. With console access disabled sshd is off and
// disable_after_boot does not apply; the sshd configuration and the
// authorized keys are still managed for when access is enabled again.
func (c *client) applySSH(d SSH, boot time.Time, consoleAccess *bool) (*SSH, time.Duration, error) {
	st := &SSH{}
	if consoleAccess != nil && !*consoleAccess {
		st.Disabled = true
		st2, err := c.applySSHConfig(d, st)
		return st2, 0, err
	}
	disabled, recheck, gateErr := c.sshGate(d.DisableAfterBoot, boot, time.Now())
	st.Disabled = disabled
	st2, err := c.applySSHConfig(d, st)
	if err == nil {
		err = gateErr
	}
	return st2, recheck, err
}

func (c *client) applySSHConfig(d SSH, st *SSH) (*SSH, error) {
	if d.AuthorizedKeys != nil {
		keys, err := c.applyKeys(d.AuthorizedKeys)
		if err != nil {
			return nil, err
		}
		st.AuthorizedKeys = keys
	}

	mode := ""
	if p := d.PasswordAuthentication; p != nil {
		mode = "no"
		if *p {
			mode = "yes"
		}
	}
	out, err := c.console("sh", "-c", sshdScript, "sh", mode)
	if err != nil {
		if mode == "" {
			// Only reading the state: not an error of the server, e.g. an
			// older console where sshd -T does not work.
			log.Printf("cannot read sshd settings: %v", err)
			return st, nil
		}
		return st, fmt.Errorf("sshd: %v", err)
	}
	if f := strings.Fields(out); len(f) == 2 {
		on := strings.EqualFold(f[1], "yes")
		st.PasswordAuthentication = &on
	}
	if mode != "" && (st.PasswordAuthentication == nil || (mode == "yes") != *st.PasswordAuthentication) {
		return st, fmt.Errorf("sshd does not apply password_authentication, console without sshd_config.d support?")
	}
	return st, nil
}

// console runs a command in the console container.
func (c *client) console(args ...string) (string, error) {
	return capture(systemDocker, append([]string{"exec", "console"}, args...)...)
}

// applyKeys writes the managed block of authorized_keys of every SSH user
// and returns the keys now in it.
func (c *client) applyKeys(keys []string) ([]string, error) {
	var applied []string
	for _, user := range sshUsers {
		home := filepath.Join(c.homeDir, user)
		fi, err := os.Stat(home)
		if os.IsNotExist(err) {
			continue
		} else if err != nil {
			return nil, err
		}
		uid, gid := -1, -1
		if st, ok := fi.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(st.Uid), int(st.Gid)
		}
		dir := filepath.Join(home, ".ssh")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, err
		}
		if err := os.Chown(dir, uid, gid); err != nil {
			return nil, err
		}
		file := filepath.Join(dir, "authorized_keys")
		old, err := os.ReadFile(file)
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		content := updateAuthorizedKeys(string(old), keys)
		if content != string(old) {
			if err := writeFile(file, content, uid, gid); err != nil {
				return nil, err
			}
			fmt.Printf("updated managed SSH keys of %s\n", user)
		}
		if applied == nil {
			applied = managedKeys(content)
		}
	}
	if applied == nil {
		applied = []string{}
	}
	return applied, nil
}

func writeFile(path, content string, uid, gid int) error {
	tmp := path + ".fleet-tmp"
	if err := os.WriteFile(tmp, []byte(content), 0600); err != nil {
		return err
	}
	if err := os.Chown(tmp, uid, gid); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

// updateAuthorizedKeys replaces the managed block of an authorized_keys
// file. Lines outside of the block are kept as they are.
func updateAuthorizedKeys(content string, keys []string) string {
	var out []string
	inBlock := false
	for _, line := range strings.Split(strings.TrimRight(content, "\n"), "\n") {
		switch {
		case line == keysBegin:
			inBlock = true
		case line == keysEnd:
			inBlock = false
		case !inBlock && (line != "" || len(out) > 0):
			out = append(out, line)
		}
	}
	var block []string
	for _, k := range keys {
		if k = strings.TrimSpace(k); k != "" && !slices.Contains(block, k) {
			block = append(block, k)
		}
	}
	if len(block) > 0 {
		out = append(out, keysBegin)
		out = append(out, block...)
		out = append(out, keysEnd)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, "\n") + "\n"
}

// managedKeys returns the keys of the managed block.
func managedKeys(content string) []string {
	keys := []string{}
	inBlock := false
	for _, line := range strings.Split(content, "\n") {
		switch {
		case line == keysBegin:
			inBlock = true
		case line == keysEnd:
			inBlock = false
		case inBlock && line != "":
			keys = append(keys, line)
		}
	}
	return keys
}
