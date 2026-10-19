# Production deployment

CI builds server and client images tagged `sha-<full commit SHA>`. The VPS checks GitHub's public Actions API every five minutes. It deploys only when the latest successful `main` push CI run matches the current `main` commit. The timer uses outbound HTTPS; GitHub does not connect to the VPS.

After pushing this deployment change, bootstrap the timer once on the VPS through the existing SSH route or the provider console:

```sh
cd /opt/macro-terminal
git pull --ff-only
install -m 0644 deploy/macro-terminal-deploy.service /etc/systemd/system/
install -m 0644 deploy/macro-terminal-deploy.timer /etc/systemd/system/
systemctl daemon-reload
systemctl enable --now macro-terminal-deploy.timer
systemctl start macro-terminal-deploy.service
systemctl status macro-terminal-deploy.service macro-terminal-deploy.timer
```

Check `journalctl -u macro-terminal-deploy.service` and the running image tags. Once a successful CI revision has deployed through the timer, remove the public SSH firewall rule. Keep ports 80 and 443 open for the application. After public SSH closes, the Contabo console remains the administrative recovery path.
