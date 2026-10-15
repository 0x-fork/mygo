package glsl

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/internal/shaderir"
)

// Fragment returns p's fragment entry point as a function named name of
// GLSL 3.30 and GLSL ES 3.00, for a renderer's fragment shader to call,
// after the uniforms (U0, U1, ...), the textures (sampler2D T0, T1, ...)
// and the functions it reaches:
//
//	vec4 name(vec4 position, ...varyings)
//
// The position counts its y from the top of the target, as the renderer
// passes it; flipY is a boolean expression of the renderer's, true where
// GL's window y counts up, as in the default framebuffer, which negates
// dFdy, or "" where it never does.
func Fragment(p *shaderir.Program, name, flipY string) string {
	lib, entry, funcs := p.FragmentLibrary()
	if entry == nil {
		return ""
	}
	c := &compileContext{version: GLSLVersionES300, projectionMatrixUniformIndex: -1, structNames: map[string]string{}, library: true, flipY: flipY}

	lines := []string{"{{.Structs}}", ""}
	for i, t := range lib.Uniforms {
		lines = append(lines, fmt.Sprintf("uniform %s;", c.varDecl(lib, &t, fmt.Sprintf("U%d", i))))
	}
	for i := 0; i < lib.TextureCount; i++ {
		lines = append(lines, fmt.Sprintf("uniform sampler2D T%d;", i))
	}
	lines = append(lines, "")
	for _, f := range funcs {
		lines = append(lines, c.function(lib, f, true)...)
	}
	for _, f := range funcs {
		lines = append(lines, "")
		lines = append(lines, c.function(lib, f, false)...)
	}

	var params, args []string
	for i, t := range entry.InParams {
		n := "position"
		if i > 0 {
			n = fmt.Sprintf("v%d", i-1)
		}
		params, args = append(params, c.varDecl(lib, &t, n)), append(args, n)
	}
	lines = append(lines, "",
		fmt.Sprintf("vec4 %s(%s) {", name, strings.Join(params, ", ")),
		fmt.Sprintf("\treturn F%d(%s);", entry.Index, strings.Join(args, ", ")),
		"}")

	ls := strings.Join(lines, "\n")
	var st []string
	for i, t := range c.structTypes {
		st = append(st, fmt.Sprintf("struct S%d {", i))
		for j, m := range t.Sub {
			st = append(st, fmt.Sprintf("\t%s;", c.varDecl(lib, &m, fmt.Sprintf("M%d", j))))
		}
		st = append(st, "};")
	}
	ls = strings.ReplaceAll(ls, "{{.Structs}}", strings.Join(st, "\n"))
	ls = regexp.MustCompile(`\n\n+`).ReplaceAllString(ls, "\n\n")
	return strings.TrimSpace(ls) + "\n"
}
