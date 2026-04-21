# reSSH

CLI tool for managing SSH SOCKS proxy tunnels in the background.

## Install

```bash
wget -qO- https://github.com/shatxme/reSSH/raw/main/install.sh | bash
```

## Commands

| Command | What it does |
| --- | --- |
| `ressh list` | Show targets from `~/.ssh/config` |
| `ressh use <target>` | Set the default target |
| `ressh on [target]` | Start the tunnel for the given target, or the default one |
| `ressh off` | Stop the tunnel |
| `ressh status` | Show current tunnel status |
| `ressh logs` | Show daemon logs |
| `ressh vps-setup --host <ip> --user root --password <password>` | Set up a VPS, create a key, and add a `ressh-*` SSH host entry |
| `ressh version` | Show the installed version |
