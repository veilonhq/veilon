package tgbot

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

var rePersian = regexp.MustCompile(`[\x{0600}-\x{06FF}]`)

// TestCatalogComplete checks that every text passed to T or Tf has an
// English translation with the same format verbs and no Persian left.
func TestCatalogComplete(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	fset := token.NewFileSet()
	verbs := regexp.MustCompile(`%[-+# 0-9.]*[sdvqxf%]`)
	n := 0
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "i18n_en.go" {
			continue
		}
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, f, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(nd ast.Node) bool {
			c, ok := nd.(*ast.CallExpr)
			if !ok || len(c.Args) == 0 {
				return true
			}
			id, ok := c.Fun.(*ast.Ident)
			if !ok || (id.Name != "T" && id.Name != "Tf") {
				return true
			}
			lit, ok := c.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				t.Errorf("%s: %s called with a non-literal text", fset.Position(c.Pos()), id.Name)
				return true
			}
			fa, _ := strconv.Unquote(lit.Value)
			n++
			en, ok := enCatalog[fa]
			switch {
			case !ok:
				t.Errorf("%s: no English for %q", fset.Position(c.Pos()), fa)
			case rePersian.MatchString(en):
				t.Errorf("English for %q has Persian: %q", fa, en)
			case id.Name == "Tf" && strings.Join(verbs.FindAllString(fa, -1), "") != strings.ReplaceAll(strings.Join(verbs.FindAllString(en, -1), ""), "%%", ""):
				t.Errorf("format verbs differ: %q / %q", fa, en)
			}
			return true
		})
	}
	if n < 500 {
		t.Fatalf("only %d texts found", n)
	}
	// Persian texts must not be left out of T (outside comments and the
	// language picker, which shows both languages).
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") || f == "i18n_en.go" || f == "i18n.go" {
			continue
		}
		file, _ := parser.ParseFile(fset, f, nil, 0)
		ast.Inspect(file, func(nd ast.Node) bool {
			if c, ok := nd.(*ast.CallExpr); ok {
				if id, ok := c.Fun.(*ast.Ident); ok && (id.Name == "T" || id.Name == "Tf" || id.Name == "both") {
					return false
				}
			}
			lit, ok := nd.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING || !rePersian.MatchString(lit.Value) {
				return true
			}
			switch {
			case strings.Contains(lit.Value, "٫"), strings.Contains(lit.Value, "فارسی|persian"), strings.Contains(lit.Value, "english|انگلیسی"):
				// input parsing and release note headings
			case strings.Contains(lit.Value, "The bot is connected"):
				// the connection test message carries both languages
			default:
				t.Errorf("%s: Persian text without T: %s", fset.Position(lit.Pos()), lit.Value)
			}
			return true
		})
	}
}

func TestLocalize(t *testing.T) {
	s := both("سلام "+both("دنیا", "world"), "hello "+both("دنیا", "world")) + " · " + fa("12")
	if got := localize(s, langEn); got != "hello world · 12" {
		t.Fatalf("en: %q", got)
	}
	if got := localize(s, langFa); got != "سلام دنیا · ۱۲" {
		t.Fatalf("fa: %q", got)
	}
	if got := localize(Tf("⚠️ عدد باید از %s تا %s باشد.\n", fa("1"), fa("9")), langEn); got != "⚠️ The number must be from 1 to 9.\n" {
		t.Fatalf("Tf: %q", got)
	}
	// cut keeps both languages whole.
	c := cut(T("کاربر جدید (۱ از ۴)")+" abc", 8)
	if got := localize(c, langEn); got != "New use…" {
		t.Fatalf("cut en: %q", got)
	}
	if got := localize(c, langFa); got != "کاربر ج…" {
		t.Fatalf("cut fa: %q", got)
	}
}
