package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/user"
	"strings"
	"time"

	"github.com/shatxme/ressh/internal/ressh"
)

var version = "dev"

func main() {
	ctx := context.Background()
	app, err := ressh.New()
	if err != nil {
		exitErr(err)
	}

	if len(os.Args) < 2 {
		printUsage()
		return
	}

	switch os.Args[1] {
	case "on":
		exitErr(runOn(ctx, app, os.Args[2:]))
	case "off":
		exitErr(app.Disconnect(ctx))
	case "status":
		exitErr(runStatus(ctx, app))
	case "list":
		exitErr(runList(app))
	case "use":
		exitErr(runUse(app, os.Args[2:]))
	case "logs":
		exitErr(runLogs(app))
	case "vps-setup":
		exitErr(runVPSSetup(ctx, app, os.Args[2:]))
	case "daemon":
		exitErr(app.RunDaemon(ctx))
	case "help", "-h", "--help":
		printUsage()
	case "version":
		fmt.Println(version)
	default:
		exitErr(fmt.Errorf("unknown command %q", os.Args[1]))
	}
}

func runOn(ctx context.Context, app *ressh.App, args []string) error {
	fs := flag.NewFlagSet("on", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	host := fs.String("host", "", "direct host to connect to")
	userFlag := fs.String("user", "", "username for direct connection")
	port := fs.Int("port", 22, "SSH port for direct connection")
	key := fs.String("key", "", "identity file for direct connection")
	noProxy := fs.Bool("no-proxy", false, "leave system proxy unchanged")
	if err := fs.Parse(args); err != nil {
		return err
	}

	proxyEnabled := !*noProxy

	if strings.TrimSpace(*host) != "" {
		username := strings.TrimSpace(*userFlag)
		if username == "" {
			currentUser, err := user.Current()
			if err != nil {
				return err
			}
			username = currentUser.Username
		}
		spec := ressh.TargetSpec{Hostname: strings.TrimSpace(*host), User: username, Port: *port, KeyFile: strings.TrimSpace(*key)}
		if err := app.Connect(ctx, spec, proxyEnabled); err != nil {
			return err
		}
		fmt.Printf("Connecting to %s@%s\n", username, strings.TrimSpace(*host))
		return nil
	}

	var rawTarget string
	if fs.NArg() > 0 {
		rawTarget = fs.Arg(0)
	}

	spec, label, err := app.ResolveTarget(rawTarget)
	if err != nil {
		return err
	}
	if err := app.Connect(ctx, spec, proxyEnabled); err != nil {
		return err
	}
	fmt.Printf("Connecting to %s\n", label)
	return nil
}

func runStatus(ctx context.Context, app *ressh.App) error {
	status, err := app.Status(ctx)
	if err != nil {
		if errors.Is(err, ressh.ErrDaemonUnavailable) {
			fmt.Println("Status: disconnected")
			return nil
		}
		return err
	}

	fmt.Printf("Status: %s\n", status.Status)
	if status.ConnectedTo != "" {
		fmt.Printf("Target: %s\n", status.ConnectedTo)
	}
	fmt.Printf("SOCKS: 127.0.0.1:%d\n", status.SocksPort)
	fmt.Printf("Proxy: %v\n", status.ProxyEnabled)
	if status.KillSwitchActive {
		fmt.Println("Kill switch: active")
	}
	if status.LastError != "" {
		fmt.Printf("Last error: %s\n", status.LastError)
	}
	return nil
}

func runList(app *ressh.App) error {
	hosts, defaultTarget, err := app.ListTargets()
	if err != nil {
		return err
	}
	if len(hosts) == 0 {
		fmt.Println("No SSH targets found in ~/.ssh/config")
		return nil
	}
	for _, host := range hosts {
		marker := " "
		if host.Alias == defaultTarget {
			marker = "*"
		}
		fmt.Printf("%s %s", marker, host.Alias)
		if host.Hostname != "" {
			fmt.Printf(" -> %s", host.Hostname)
		}
		if host.User != "" {
			fmt.Printf(" (%s", host.User)
			if host.Port > 0 {
				fmt.Printf(":%d", host.Port)
			}
			fmt.Print(")")
		}
		fmt.Println()
	}
	return nil
}

func runUse(app *ressh.App, args []string) error {
	if len(args) != 1 {
		return errors.New("usage: ressh use <target>")
	}
	if err := app.SetDefaultTarget(args[0]); err != nil {
		return err
	}
	fmt.Printf("Default target set to %s\n", args[0])
	return nil
}

func runLogs(app *ressh.App) error {
	content, err := app.Logs()
	if err != nil {
		return err
	}
	fmt.Print(content)
	return nil
}

func runVPSSetup(ctx context.Context, app *ressh.App, args []string) error {
	fs := flag.NewFlagSet("vps-setup", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	host := fs.String("host", "", "VPS hostname or IP")
	userFlag := fs.String("user", "root", "login user for setup")
	password := fs.String("password", "", "root password (insecure, visible in process list)")
	passwordStdin := fs.Bool("password-stdin", false, "read password from stdin")
	name := fs.String("name", "", "optional ssh alias label")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if strings.TrimSpace(*host) == "" {
		return errors.New("--host is required")
	}

	secret, err := ressh.ResolvePassword(strings.TrimSpace(*password), *passwordStdin)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()

	result, err := app.VPSSetup(ctx, ressh.VPSSetupInput{
		Host:     strings.TrimSpace(*host),
		User:     strings.TrimSpace(*userFlag),
		Password: secret,
		Name:     strings.TrimSpace(*name),
	})
	if err != nil {
		return err
	}

	fmt.Printf("SSH alias: %s\n", result.Alias)
	fmt.Printf("Key file: %s\n", result.KeyFile)
	fmt.Printf("Default target: %s\n", result.DefaultTarget)
	return nil
}

func printUsage() {
	fmt.Print(`reSSH

Usage:
  ressh on [target]
  ressh on --host <host> [--user <user>] [--port 22] [--key ~/.ssh/key]
  ressh off
  ressh status
  ressh list
  ressh use <target>
  ressh logs
  ressh vps-setup --host <ip> [--user root]
  ressh version

Notes:
  - Targets come from ~/.ssh/config.
  - 'ressh on' uses the default target set via 'ressh use'.
  - The CLI talks to a background daemon, so no terminal stays open.
`)
}

func exitErr(err error) {
	if err == nil {
		return
	}
	fmt.Fprintln(os.Stderr, "Error:", err)
	os.Exit(1)
}
