package main

import (
	"os"
)

func runGenerate(args []string) error {
	fs := newFlags("generate", "[-o file] [dir]", "Writes the typed TypeScript client for the services bound with mygo.Bind\nand the events declared with mygo.NewEvent.")
	out := fs.String("o", "", "output file (default: bindings from mygo.json)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	c, err := loadConfig(dirArg(fs.Args()))
	if err != nil {
		return err
	}
	if *out != "" {
		c.Bindings = *out
	}
	bin := tempBinary(c.executableName())
	defer os.Remove(bin)
	if err := buildBinary(c, bin, nil); err != nil {
		return err
	}
	return generateBindings(c, bin)
}

func dirArg(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return "."
}
