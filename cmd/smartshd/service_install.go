package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/BegaDeveloper/smartsh/internal/runtimeconfig"
)

func installService() error {
	switch runtime.GOOS {
	case "darwin":
		return installLaunchdService()
	case "linux":
		return installSystemdUserService()
	case "windows":
		return installWindowsTaskService()
	default:
		return fmt.Errorf("install-service is not supported on %s", runtime.GOOS)
	}
}

func serviceEnv() map[string]string {
	configValues := map[string]string{}
	config, configErr := runtimeconfig.Load("")
	if configErr == nil {
		configValues = config.Values
	}
	values := map[string]string{
		"SMARTSH_DAEMON_ADDR":  runtimeconfig.ResolveString("SMARTSH_DAEMON_ADDR", configValues),
		"SMARTSH_DAEMON_TOKEN": runtimeconfig.ResolveString("SMARTSH_DAEMON_TOKEN", configValues),
	}
	if strings.TrimSpace(values["SMARTSH_DAEMON_ADDR"]) == "" {
		values["SMARTSH_DAEMON_ADDR"] = "127.0.0.1:8787"
	}
	if runtimeconfig.ResolveBool("SMARTSH_DAEMON_DISABLE_AUTH", configValues) {
		values["SMARTSH_DAEMON_DISABLE_AUTH"] = "true"
	}
	return values
}

func installLaunchdService() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")
	if err := os.MkdirAll(launchAgentsDir, 0o755); err != nil {
		return err
	}
	plistPath := filepath.Join(launchAgentsDir, "com.smartsh.smartshd.plist")
	logDir := filepath.Join(homeDir, ".smartsh")
	_ = os.MkdirAll(logDir, 0o700)
	env := serviceEnv()
	envEntries := make([]string, 0, len(env))
	for key, value := range env {
		if strings.TrimSpace(value) == "" {
			continue
		}
		envEntries = append(envEntries, "<key>"+key+"</key><string>"+xmlEscape(value)+"</string>")
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>com.smartsh.smartshd</string>
  <key>ProgramArguments</key>
  <array><string>` + xmlEscape(executable) + `</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>` + xmlEscape(filepath.Join(logDir, "smartshd.log")) + `</string>
  <key>StandardErrorPath</key><string>` + xmlEscape(filepath.Join(logDir, "smartshd.err.log")) + `</string>
  <key>EnvironmentVariables</key><dict>` + strings.Join(envEntries, "") + `</dict>
</dict>
</plist>
`
	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if output, runErr := exec.Command("launchctl", "load", plistPath).CombinedOutput(); runErr != nil {
		return fmt.Errorf("launchctl load failed: %v (%s)", runErr, strings.TrimSpace(string(output)))
	}
	return nil
}

func installSystemdUserService() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	unitDir := filepath.Join(homeDir, ".config", "systemd", "user")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		return err
	}
	unitPath := filepath.Join(unitDir, "smartshd.service")
	env := serviceEnv()
	envLines := make([]string, 0, len(env))
	for key, value := range env {
		if strings.TrimSpace(value) == "" {
			continue
		}
		envLines = append(envLines, `Environment="`+key+"="+systemdEscape(value)+`"`)
	}
	unit := `[Unit]
Description=smartsh local daemon
After=network.target

[Service]
Type=simple
ExecStart=` + executable + `
Restart=always
RestartSec=2
` + strings.Join(envLines, "\n") + `

[Install]
WantedBy=default.target
`
	if err := os.WriteFile(unitPath, []byte(unit), 0o644); err != nil {
		return err
	}
	if output, runErr := exec.Command("systemctl", "--user", "daemon-reload").CombinedOutput(); runErr != nil {
		return fmt.Errorf("systemctl daemon-reload failed: %v (%s)", runErr, strings.TrimSpace(string(output)))
	}
	if output, runErr := exec.Command("systemctl", "--user", "enable", "--now", "smartshd.service").CombinedOutput(); runErr != nil {
		return fmt.Errorf("systemctl enable/start failed: %v (%s)", runErr, strings.TrimSpace(string(output)))
	}
	return nil
}

func installWindowsTaskService() error {
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	scriptPath := filepath.Join(homeDir, ".smartsh", "smartshd-service.ps1")
	if err := os.MkdirAll(filepath.Dir(scriptPath), 0o700); err != nil {
		return err
	}
	env := serviceEnv()
	lines := []string{}
	for key, value := range env {
		if strings.TrimSpace(value) == "" {
			continue
		}
		lines = append(lines, `$env:`+key+` = "`+powerShellEscape(value)+`"`)
	}
	lines = append(lines, `Start-Process -WindowStyle Hidden -FilePath "`+powerShellEscape(executable)+`"`)
	script := strings.Join(lines, "\r\n") + "\r\n"
	if err := os.WriteFile(scriptPath, []byte(script), 0o644); err != nil {
		return err
	}
	taskCommand := `powershell -NoProfile -ExecutionPolicy Bypass -File "` + scriptPath + `"`
	createArgs := []string{"/Create", "/TN", "smartshd", "/SC", "ONLOGON", "/TR", taskCommand, "/F"}
	if output, runErr := exec.Command("schtasks", createArgs...).CombinedOutput(); runErr != nil {
		return fmt.Errorf("schtasks create failed: %v (%s)", runErr, strings.TrimSpace(string(output)))
	}
	_, _ = exec.Command("schtasks", "/Run", "/TN", "smartshd").CombinedOutput()
	return nil
}

func xmlEscape(value string) string {
	escaped := strings.ReplaceAll(value, "&", "&amp;")
	escaped = strings.ReplaceAll(escaped, "<", "&lt;")
	escaped = strings.ReplaceAll(escaped, ">", "&gt;")
	return escaped
}

func systemdEscape(value string) string {
	return strings.ReplaceAll(value, `"`, `\"`)
}
