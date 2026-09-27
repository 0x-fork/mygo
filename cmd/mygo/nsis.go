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
SectionEnd

Section "Uninstall"
  Delete "$SMPROGRAMS\` + nsisEscape(fsName(c.Name)) + `.lnk"
  RMDir /r "$INSTDIR"
  DeleteRegKey HKCU ` + nsisString(uninstallKey) + `
SectionEnd
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

// nsisEscape escapes text for an NSIS string in double quotes.
func nsisEscape(s string) string {
	return strings.NewReplacer(`$`, `$$`, `"`, `$\"`, "\n", `$\n`, "\r", `$\r`, "\t", `$\t`).Replace(s)
}

func nsisString(s string) string { return `"` + nsisEscape(s) + `"` }
