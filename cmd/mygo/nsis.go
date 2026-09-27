package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The Windows installer is built with NSIS (makensis) when it is installed:
// a per-user install in %LOCALAPPDATA%\Programs, where the app can update
// itself, with a Start menu shortcut and an uninstaller that Settings >
// Apps lists.

// makensis finds the NSIS compiler.
func makensis() string {
	if p, err := exec.LookPath("makensis"); err == nil {
		return p
	}
	if runtime.GOOS == "windows" {
		for _, env := range []string{"ProgramFiles(x86)", "ProgramFiles"} {
			if p := filepath.Join(os.Getenv(env), "NSIS", "makensis.exe"); fileExists(p) {
				return p
			}
		}
	}
	return ""
}

// writeInstaller builds "<Name> Setup <version>.exe" in stage from the app
// files installed, the executable exe among them, and returns its path, or
// "" without NSIS.
func writeInstaller(c *Config, stage, work, exe string, installed []string) (string, error) {
	tool := makensis()
	if tool == "" {
		logf("skipping the Windows installer: install NSIS (makensis)")
		return "", nil
	}
	out := filepath.Join(stage, fsName(c.Name)+" Setup "+fsName(c.Version)+".exe")
	var files strings.Builder
	for _, name := range installed {
		p := filepath.Join(stage, name)
		if isDir(p) {
			fmt.Fprintf(&files, "  File /r %s\n", nsisString(p))
		} else {
			fmt.Fprintf(&files, "  File %s\n", nsisString(p))
		}
	}
	icon := ""
	if c.Icon != "" {
		src, err := os.ReadFile(c.path(c.Icon))
		if err != nil {
			return "", err
		}
		ico, err := pngToICO(src)
		if err != nil {
			return "", err
		}
		path := filepath.Join(work, "installer.ico")
		if err := os.WriteFile(path, ico, 0o644); err != nil {
			return "", err
		}
		icon = "!define MUI_ICON " + nsisString(path) + "\n!define MUI_UNICON " + nsisString(path) + "\n"
	}
	uninstallKey := `Software\Microsoft\Windows\CurrentVersion\Uninstall\` + c.Identifier
	register, unregister := nsisAssociations(c, exe)
	script := `Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma
Name ` + nsisString(c.Name) + `
OutFile ` + nsisString(out) + `
InstallDir "$LOCALAPPDATA\Programs\` + nsisEscape(fsName(c.Name)) + `"
RequestExecutionLevel user
BrandingText " "
` + icon + `!define MUI_FINISHPAGE_RUN "$INSTDIR\` + nsisEscape(exe) + `"
!include "MUI2.nsh"
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Section
  SetOutPath "$INSTDIR"
` + files.String() + `  WriteUninstaller "$INSTDIR\Uninstall.exe"
  CreateShortCut "$SMPROGRAMS\` + nsisEscape(fsName(c.Name)) + `.lnk" "$INSTDIR\` + nsisEscape(exe) + `"
  WriteRegStr HKCU ` + nsisString(uninstallKey) + ` "DisplayName" ` + nsisString(c.Name) + `
  WriteRegStr HKCU ` + nsisString(uninstallKey) + ` "DisplayVersion" ` + nsisString(c.Version) + `
  WriteRegStr HKCU ` + nsisString(uninstallKey) + ` "DisplayIcon" "$INSTDIR\` + nsisEscape(exe) + `"
  WriteRegStr HKCU ` + nsisString(uninstallKey) + ` "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU ` + nsisString(uninstallKey) + ` "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU ` + nsisString(uninstallKey) + ` "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU ` + nsisString(uninstallKey) + ` "NoModify" 1
  WriteRegDWORD HKCU ` + nsisString(uninstallKey) + ` "NoRepair" 1
` + register + `SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\` + nsisEscape(fsName(c.Name)) + `.lnk"
  RMDir /r "$INSTDIR"
  DeleteRegKey HKCU ` + nsisString(uninstallKey) + `
` + unregister + `SectionEnd
`
	nsi := filepath.Join(work, "installer.nsi")
	if err := os.WriteFile(nsi, []byte(script), 0o644); err != nil {
		return "", err
	}
	logf("creating %s", filepath.Base(out))
	if out, err := exec.Command(tool, "-V2", nsi).CombinedOutput(); err != nil {
		return "", fmt.Errorf("makensis: %v\n%s", err, out)
	}
	return out, nil
}

// nsisAssociations returns the installer commands that register, and
// unregister, the file associations and URL schemes of the app for the
// user, opening them with exe.
func nsisAssociations(c *Config, exe string) (register, unregister string) {
	var r, u strings.Builder
	open := `'"$INSTDIR\` + nsisEscape(exe) + `" "%1"'`
	prefix := strings.Map(func(r rune) rune {
		if r < 0x80 && (r == '.' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return r
		}
		return -1
	}, c.Identifier)
	for _, fa := range c.FileAssociations {
		for _, ext := range fa.Ext {
			progID := prefix + "." + strings.ToLower(ext)
			class := `Software\Classes\` + progID
			fmt.Fprintf(&r, "  WriteRegStr HKCU %s \"\" %s\n", nsisString(class), nsisString(fa.Name))
			fmt.Fprintf(&r, "  WriteRegStr HKCU %s \"\" \"$INSTDIR\\%s,0\"\n", nsisString(class+`\DefaultIcon`), nsisEscape(exe))
			fmt.Fprintf(&r, "  WriteRegStr HKCU %s \"\" %s\n", nsisString(class+`\shell\open\command`), open)
			fmt.Fprintf(&r, "  WriteRegStr HKCU %s %s \"\"\n", nsisString(`Software\Classes\.`+ext+`\OpenWithProgids`), nsisString(progID))
			fmt.Fprintf(&u, "  DeleteRegKey HKCU %s\n", nsisString(class))
			fmt.Fprintf(&u, "  DeleteRegValue HKCU %s %s\n", nsisString(`Software\Classes\.`+ext+`\OpenWithProgids`), nsisString(progID))
		}
	}
	for _, scheme := range c.URLSchemes {
		key := `Software\Classes\` + scheme
		fmt.Fprintf(&r, "  WriteRegStr HKCU %s \"\" %s\n", nsisString(key), nsisString("URL:"+c.Name))
		fmt.Fprintf(&r, "  WriteRegStr HKCU %s \"URL Protocol\" \"\"\n", nsisString(key))
		fmt.Fprintf(&r, "  WriteRegStr HKCU %s \"\" %s\n", nsisString(key+`\shell\open\command`), open)
		fmt.Fprintf(&u, "  DeleteRegKey HKCU %s\n", nsisString(key))
	}
	if r.Len() > 0 {
		// Tell Explorer the associations changed.
		notify := "  System::Call 'shell32::SHChangeNotify(i 0x08000000, i 0, p 0, p 0)'\n"
		r.WriteString(notify)
		u.WriteString(notify)
	}
	return r.String(), u.String()
}

// nsisEscape escapes text for an NSIS string in double quotes.
func nsisEscape(s string) string {
	return strings.NewReplacer(`$`, `$$`, `"`, `$\"`, "\n", `$\n`, "\r", `$\r`, "\t", `$\t`).Replace(s)
}

func nsisString(s string) string { return `"` + nsisEscape(s) + `"` }
