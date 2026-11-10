package main

// Keep /root/veilon-credentials.txt (written by the installer) in step with
// CLI changes to the admin login, the panel port and the panel path, so the
// file never shows a password or URL that no longer works.

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// credentialChanging reports whether a command changes what the credentials
// file records.
func credentialChanging(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "set-port", "set-path":
		return true
	case "admin":
		return len(args) > 1 && (args[1] == "reset-credentials" || args[1] == "reset-password")
	}
	return false
}

// runRefreshingCredFile runs the command as a child (which drops to the
// service user as usual), then updates the credentials file as root. It
// returns the exit code to use.
func runRefreshingCredFile() int {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	var out bytes.Buffer
	cmd := exec.Command(self, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "VEILON_CRED_CHILD=1")
	cmd.Stdin, cmd.Stderr = os.Stdin, os.Stderr
	cmd.Stdout = io.MultiWriter(os.Stdout, &out)
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return ee.ExitCode()
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		return 1
	}
	if err := refreshCredFile(credFile, os.Args[1:], out.String(), func() string {
		u, err := selfOutputQuiet("info", "--url")
		if err != nil {
			return ""
		}
		return strings.TrimSpace(string(u))
	}); err != nil {
		warnf("could not update %s: %v", credFile, err)
	}
	return 0
}

// refreshCredFile rewrites path (if it exists) after a successful command:
// args are the command's arguments, out its standard output and url returns
// the current panel URL.
func refreshCredFile(path string, args []string, out string, url func() string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	fields := map[string]string{}
	for _, l := range strings.Split(string(raw), "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok {
			fields[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	printed := map[string]string{}
	for _, l := range strings.Split(out, "\n") {
		if k, v, ok := strings.Cut(l, ":"); ok {
			printed[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	switch {
	case len(args) > 1 && args[0] == "admin" && args[1] == "reset-credentials":
		target := flagValue(args[2:], "current")
		if target != "" && target != fields["Username"] {
			break // another admin changed: the file keeps its own login
		}
		if u := printed["username"]; u != "" {
			fields["Username"] = u
		}
		if p := printed["password"]; p != "" {
			fields["Password"] = p
		}
	case len(args) > 1 && args[0] == "admin" && args[1] == "reset-password":
		if flagValue(args[2:], "username") != fields["Username"] {
			break
		}
		p := flagValue(args[2:], "password")
		if p == "" {
			p = printed["password"]
		}
		if p != "" {
			fields["Password"] = p
		}
	}
	if u := url(); u != "" {
		fields["URL"] = u
	}
	content := fmt.Sprintf("Veilon panel\nURL:      %s\nUsername: %s\nPassword: %s\n", fields["URL"], fields["Username"], fields["Password"])
	if content == string(raw) {
		return nil
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".veilon-credentials-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// flagValue returns the value of --name / -name (separate or with =).
func flagValue(args []string, name string) string {
	for i, a := range args {
		for _, p := range []string{"--" + name, "-" + name} {
			if a == p && i+1 < len(args) {
				return args[i+1]
			}
			if strings.HasPrefix(a, p+"=") {
				return strings.TrimPrefix(a, p+"=")
			}
		}
	}
	return ""
}
