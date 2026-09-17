# contrib

`tailnet-autoserve` - exposes every local TCP listener to your tailnet on the same port, and
removes the entry when the listener goes away. Run it from a user systemd timer every 10 s:

```
[Unit]
Description=Re-scan local listeners every 10s
[Timer]
OnBootSec=10
OnUnitActiveSec=10
AccuracySec=1
[Install]
WantedBy=timers.target
```

Edit `SKIP` for ports that must stay local. Needs passwordless `sudo tailscale`.
