package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRefreshCredFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cred.txt")
	orig := "Veilon panel\nURL:      https://1.2.3.4:100/a/\nUsername: admin\nPassword: old\n"
	write := func() {
		if err := os.WriteFile(p, []byte(orig), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	read := func() string { b, _ := os.ReadFile(p); return string(b) }
	url := func() string { return "https://1.2.3.4:200/b/" }
	none := func() string { return "" }

	write()
	if err := refreshCredFile(p, []string{"admin", "reset-credentials", "--username", "boss"}, "username: boss\npassword: NewPass1\n", none); err != nil {
		t.Fatal(err)
	}
	if got, want := read(), "Veilon panel\nURL:      https://1.2.3.4:100/a/\nUsername: boss\nPassword: NewPass1\n"; got != want {
		t.Fatalf("reset-credentials:\n%s", got)
	}
	write()
	_ = refreshCredFile(p, []string{"admin", "reset-password", "--username", "admin", "--password", "Given-1"}, "password for \"admin\" updated\n", none)
	if read() != "Veilon panel\nURL:      https://1.2.3.4:100/a/\nUsername: admin\nPassword: Given-1\n" {
		t.Fatalf("reset-password: %s", read())
	}
	write()
	_ = refreshCredFile(p, []string{"admin", "reset-password", "--username", "other", "--password", "x"}, "", none)
	if read() != orig {
		t.Fatalf("other admin must not change the file: %s", read())
	}
	write()
	_ = refreshCredFile(p, []string{"admin", "reset-credentials", "--current", "other"}, "username: other\npassword: zz\n", none)
	if read() != orig {
		t.Fatalf("--current other must not change the file: %s", read())
	}
	write()
	_ = refreshCredFile(p, []string{"set-port", "200"}, "panel port set to 200\n", url)
	if read() != "Veilon panel\nURL:      https://1.2.3.4:200/b/\nUsername: admin\nPassword: old\n" {
		t.Fatalf("set-port: %s", read())
	}
	if st, _ := os.Stat(p); st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if err := refreshCredFile(filepath.Join(t.TempDir(), "missing"), []string{"set-port", "1"}, "", url); err != nil {
		t.Fatalf("missing file: %v", err)
	}
}
