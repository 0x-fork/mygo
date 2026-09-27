package main

import (
	"bytes"
	"embed"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"text/template"
)

//go:embed all:template
var templateFS embed.FS

type templateData struct {
	Name       string
	Slug       string
	Module     string
	Identifier string
	// Runtime is the version of the mygo-runtime npm package.
	Runtime string
	// CLI is the version of the mygo-cli npm package, which runs mygo in
	// the scripts of package.json, and Mygo the command they run.
	CLI, Mygo string
}

func runInit(args []string) error {
	flags := newFlags("init", "[flags] <dir>", "Creates a new MyGo project with a TypeScript frontend built with Vite.\nBun installs its dependencies, the mygo-cli package among them, and runs\nits scripts: bun run dev and bun run build.")
	name := flags.String("name", "", "application name (default: directory name)")
	module := flags.String("module", "", "Go module path (default: directory name)")
	local := flags.String("mygo", "", "path to a local checkout of MyGo to use via a replace directive")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 1 {
		flags.Usage()
		return flag2Err("missing project directory")
	}
	dir, err := filepath.Abs(flags.Arg(0))
	if err != nil {
		return err
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 {
		return fmt.Errorf("%s is not empty", dir)
	}
	base := filepath.Base(dir)
	data := templateData{Name: *name, Module: *module}
	if data.Name == "" {
		data.Name = base
	}
	data.Slug = slugify(data.Name)
	if data.Module == "" {
		data.Module = data.Slug
	}
	data.Identifier = "com.example." + strings.ReplaceAll(data.Slug, "-", "")
	data.Runtime, data.CLI, data.Mygo = "^"+version, "^"+version, "mygo"
	if *local != "" {
		// The runtime package of the local checkout, like the Go module,
		// and its CLI, which go run builds from the replaced module.
		abs, err := filepath.Abs(filepath.Join(*local, "packages", "runtime"))
		if err != nil {
			return err
		}
		data.Runtime, data.CLI, data.Mygo = "file:"+abs, "", "go run github.com/egoist/mygo/cmd/mygo"
	}

	logf("creating %s", dir)
	if err := writeTemplate(dir, data); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(dir, resourcesDir), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, resourcesDir, "icon.png"), defaultIcon(), 0o644); err != nil {
		return err
	}

	gomod := fmt.Sprintf("module %s\n\ngo %s\n", data.Module, goVersion())
	if *local != "" {
		abs, err := filepath.Abs(*local)
		if err != nil {
			return err
		}
		gomod += fmt.Sprintf("\nrequire github.com/egoist/mygo v0.0.0\n\nreplace github.com/egoist/mygo => %s\n", abs)
	}
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(gomod), 0o644); err != nil {
		return err
	}
	if *local == "" {
		if err := goCommand(dir, nil, "get", "github.com/egoist/mygo@latest").Run(); err != nil {
			logf("could not fetch github.com/egoist/mygo: %v", err)
		}
	}
	if err := goCommand(dir, nil, "mod", "tidy").Run(); err != nil {
		logf("go mod tidy failed: %v", err)
	}

	if _, err := exec.LookPath("bun"); err != nil {
		logf("bun not found: install it from https://bun.sh, then run bun install")
	} else if err := run(dir, "bun install"); err != nil {
		return err
	}
	c, err := loadConfig(dir)
	if err != nil {
		return err
	}
	bin := tempBinary(c.executableName())
	defer os.Remove(bin)
	if err := buildBinary(c, bin, nil); err == nil {
		_ = generateBindings(c, bin)
	}

	rel := dir
	if wd, err := os.Getwd(); err == nil {
		if r, err := filepath.Rel(wd, dir); err == nil && !strings.HasPrefix(r, "..") {
			rel = r
		}
	}
	fmt.Printf("\nCreated %s. Next steps:\n\n  cd %s\n  bun run dev      # develop with live reload\n  bun run build    # package the app\n\n", data.Name, rel)
	return nil
}

func writeTemplate(dir string, data templateData) error {
	return fs.WalkDir(templateFS, "template", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel("template", path)
		target := filepath.Join(dir, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		content, err := templateFS.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.HasSuffix(target, ".tmpl") {
			target = strings.TrimSuffix(target, ".tmpl")
			t, err := template.New(rel).Parse(string(content))
			if err != nil {
				return err
			}
			var b bytes.Buffer
			if err := t.Execute(&b, data); err != nil {
				return err
			}
			content = b.Bytes()
		}
		if filepath.Base(target) == "gitignore" {
			target = filepath.Join(filepath.Dir(target), ".gitignore")
		}
		return os.WriteFile(target, content, 0o644)
	})
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slugify(s string) string {
	s = strings.Trim(slugRe.ReplaceAllString(strings.ToLower(s), "-"), "-")
	if s == "" {
		return "app"
	}
	return s
}

func goVersion() string {
	v := strings.TrimPrefix(runtime.Version(), "go")
	if i := strings.IndexAny(v, " -"); i >= 0 {
		v = v[:i]
	}
	return v
}

type flagErr string

func (e flagErr) Error() string { return string(e) }

func flag2Err(s string) error { return flagErr(s) }

// defaultIcon draws a 1024x1024 app icon: a rounded square with a diagonal
// gradient and a ring.
func defaultIcon() []byte {
	const size = 1024
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	from := [3]float64{59, 130, 246} // blue
	to := [3]float64{168, 85, 247}   // violet
	inset, radius := 100.0, 185.0
	for y := range size {
		for x := range size {
			fx, fy := float64(x)+0.5, float64(y)+0.5
			// Signed distance to a rounded rectangle, for anti-aliasing.
			qx := math.Abs(fx-size/2) - (size/2 - inset - radius)
			qy := math.Abs(fy-size/2) - (size/2 - inset - radius)
			d := math.Hypot(math.Max(qx, 0), math.Max(qy, 0)) + math.Min(math.Max(qx, qy), 0) - radius
			alpha := math.Min(math.Max(0.5-d, 0), 1)
			if alpha == 0 {
				continue
			}
			t := (fx + fy) / (2 * size)
			c := [3]float64{}
			for i := range c {
				c[i] = from[i] + (to[i]-from[i])*t
			}
			// A white ring in the middle.
			r := math.Hypot(fx-size/2, fy-size/2)
			ring := math.Min(math.Max(0.5-math.Abs(r-230)+48, 0), 1)
			for i := range c {
				c[i] = c[i]*(1-ring) + 255*ring
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(c[0]), G: uint8(c[1]), B: uint8(c[2]), A: uint8(alpha * 255)})
		}
	}
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}
