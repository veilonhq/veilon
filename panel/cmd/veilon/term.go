package main

// Terminal helpers for the CLI and the management menu: colors, prompts,
// TTY detection and running the veilon binary or system tools.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"strings"
	"syscall"
)

func isCharDevice(f *os.File) bool {
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// interactive reports whether both stdin and stdout are terminals.
func interactive() bool { return isCharDevice(os.Stdin) && isCharDevice(os.Stdout) }

var useColor = isCharDevice(os.Stdout) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"

func paint(code, s string) string {
	if !useColor {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func bold(s string) string   { return paint("1", s) }
func dim(s string) string    { return paint("2", s) }
func red(s string) string    { return paint("31", s) }
func green(s string) string  { return paint("32", s) }
func yellow(s string) string { return paint("33", s) }
func cyan(s string) string   { return paint("36", s) }

func okf(format string, a ...any)   { fmt.Println(green("✔ ") + fmt.Sprintf(format, a...)) }
func warnf(format string, a ...any) { fmt.Fprintln(os.Stderr, yellow("! ")+fmt.Sprintf(format, a...)) }
func stepf(format string, a ...any) { fmt.Println(cyan("› ") + fmt.Sprintf(format, a...)) }

var stdin = bufio.NewReader(os.Stdin)

var errAborted = errors.New("aborted")

// prompt asks a question and returns the trimmed answer, or def on Enter.
func prompt(question, def string) (string, error) {
	if def != "" {
		fmt.Printf("%s %s: ", question, dim("["+def+"]"))
	} else {
		fmt.Printf("%s: ", question)
	}
	line, err := stdin.ReadString('\n')
	if err != nil && line == "" {
		fmt.Println()
		return "", errAborted
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return def, nil
	}
	return line, nil
}

// confirm asks a yes/no question.
func confirm(question string, def bool) bool {
	hint := "y/N"
	if def {
		hint = "Y/n"
	}
	ans, err := prompt(question+" "+dim("("+hint+")"), "")
	if err != nil {
		return false
	}
	switch strings.ToLower(ans) {
	case "":
		return def
	case "y", "yes":
		return true
	}
	return false
}

func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("this command must be run as root (sudo veilon ...)")
	}
	return nil
}

// runAttached runs a command with the terminal attached. Ctrl+C stops the
// child (e.g. "journalctl -f") but not this process.
func runAttached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sig)
	return cmd.Run()
}

// runSelf runs this binary with args (DB commands re-exec themselves as the
// service user from there).
func runSelf(args ...string) error {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	return runAttached(self, args...)
}

// selfOutput runs this binary and returns its stdout.
func selfOutput(args ...string) ([]byte, error) {
	self, err := os.Executable()
	if err != nil {
		self = os.Args[0]
	}
	cmd := exec.Command(self, args...)
	cmd.Stderr = os.Stderr
	return cmd.Output()
}

// serviceUser is the system account the service runs as.
const serviceUser = "veilon"

// dropToServiceUser re-executes the current command as the service user when
// running as root, so database files (SQLite) and anything the command
// creates stay owned by the service. It returns only when no re-exec is
// needed or possible.
func dropToServiceUser() {
	if os.Geteuid() != 0 || os.Getenv("VEILON_NO_REEXEC") == "1" {
		return
	}
	if _, err := user.Lookup(serviceUser); err != nil {
		return
	}
	runuser, err := exec.LookPath("runuser")
	if err != nil {
		return
	}
	self, err := os.Executable()
	if err != nil {
		return
	}
	env := append(os.Environ(), "VEILON_NO_REEXEC=1")
	if os.Getenv("VEILON_CONFIG") == "" {
		env = append(env, "VEILON_CONFIG=/etc/veilon/veilon.env")
	}
	argv := append([]string{"runuser", "-u", serviceUser, "--", self}, os.Args[1:]...)
	if err := syscall.Exec(runuser, argv, env); err != nil {
		warnf("could not switch to the %s user (%v); continuing as root", serviceUser, err)
	}
}
