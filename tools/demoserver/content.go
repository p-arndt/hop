package main

// The invented contents of the demo filesystem and the fake shell's command output.

const dockerCompose = `services:
  web:
    image: ghcr.io/example/web:1.8.2
    restart: unless-stopped
    ports:
      - "127.0.0.1:8080:8080"
    env_file: .env
    depends_on: [db, cache]

  db:
    image: postgres:16-alpine
    volumes:
      - pgdata:/var/lib/postgresql/data
    environment:
      POSTGRES_DB: example
      POSTGRES_USER: example

  cache:
    image: redis:7-alpine
    command: redis-server --save 60 1

volumes:
  pgdata:
`

const deployScript = `#!/usr/bin/env bash
set -euo pipefail

# Pull the tagged image, roll the web container, keep the old one until the new
# one answers /healthz.
tag="${1:?usage: deploy.sh <tag>}"

echo "==> pulling ${tag}"
docker compose pull web

echo "==> rolling web"
docker compose up -d --no-deps web

for i in $(seq 1 30); do
  if curl -fsS localhost:8080/healthz >/dev/null; then
    echo "==> healthy after ${i}s"
    exit 0
  fi
  sleep 1
done

echo "==> unhealthy, rolling back" >&2
docker compose rollback web || docker compose up -d --no-deps web
exit 1
`

const envExample = `# Copy to .env and fill in. Never commit the filled-in copy.
APP_ENV=production
APP_SECRET=

DATABASE_URL=postgresql://example@db:5432/example
REDIS_URL=redis://cache:6379/0

SMTP_HOST=smtp.example.com
SMTP_PORT=587
`

const readmeFile = `# example.com — web tier

Two nodes behind Caddy; this is node 1. Deploys are ` + "`./deploy.sh <tag>`" + `,
which rolls the web container only and rolls back if /healthz stays quiet for
30s.

## Runbook

- Logs:      docker compose logs -f web
- Restart:   docker compose restart web
- Backups:   nightly at 03:15 UTC -> ~/backups (14 days kept)
`

const notesFile = `- rotate the deploy key before the end of the quarter
- caddy 2.8 dropped the old on_demand_tls syntax; config already migrated
- staging still on postgres 15, bump when the web tier is settled
`

const mainPy = `import os

from flask import Flask, jsonify, render_template

app = Flask(__name__)
app.config["SECRET_KEY"] = os.environ["APP_SECRET"]


@app.get("/healthz")
def healthz():
    """Liveness probe. Cheap on purpose: deploy.sh polls it once a second."""
    return jsonify(status="ok", version=os.getenv("APP_VERSION", "dev"))


@app.get("/")
def index():
    return render_template("index.html", title="example.com")


if __name__ == "__main__":
    app.run(host="0.0.0.0", port=8080)
`

const accessLog = `10.0.4.18 - - [21/Jul/2026:09:12:03 +0000] "GET / HTTP/2.0" 200 4821 "-" "Mozilla/5.0"
10.0.4.18 - - [21/Jul/2026:09:12:03 +0000] "GET /static/style.css HTTP/2.0" 200 612 "-" "Mozilla/5.0"
10.0.4.91 - - [21/Jul/2026:09:12:11 +0000] "GET /healthz HTTP/1.1" 200 58 "-" "curl/8.7.1"
10.0.4.18 - - [21/Jul/2026:09:12:44 +0000] "POST /api/orders HTTP/2.0" 201 187 "-" "Mozilla/5.0"
10.0.4.91 - - [21/Jul/2026:09:13:11 +0000] "GET /healthz HTTP/1.1" 200 58 "-" "curl/8.7.1"
`

// ---- fake command output ----

// The motd the fake shell prints when a session opens.
const motd = "Welcome to Ubuntu 24.04.2 LTS (GNU/Linux 6.8.0-45-generic x86_64)\r\n" +
	"\r\n" +
	"  System load:  0.18          Processes:             142\r\n" +
	"  Usage of /:   41.2% of 78G  Users logged in:       1\r\n" +
	"  Memory usage: 37%           IPv4 address for eth0: 10.0.4.7\r\n" +
	"\r\n" +
	"Last login: Tue Jul 21 09:04:11 2026 from 10.0.4.18\r\n"

// commands is what the fake shell answers with; keys are matched exactly.
var commands = map[string]string{
	"ls": "\x1b[1;34mapp\x1b[0m  \x1b[1;34mbackups\x1b[0m  \x1b[1;34mlogs\x1b[0m  " +
		"\x1b[1;32mdeploy.sh\x1b[0m  docker-compose.yml  notes.txt  README.md\r\n",

	"ls -la": "total 44\r\n" +
		"drwxr-xr-x  6 deploy deploy 4096 Jul 21 09:14 \x1b[1;34m.\x1b[0m\r\n" +
		"drwxr-xr-x  3 root   root   4096 Mar 02 11:20 \x1b[1;34m..\x1b[0m\r\n" +
		"-rw-r--r--  1 deploy deploy  241 Jul 09 16:02 .env.example\r\n" +
		"drwxr-xr-x  3 deploy deploy 4096 Jul 19 22:41 \x1b[1;34mapp\x1b[0m\r\n" +
		"drwxr-xr-x  2 deploy deploy 4096 Jul 17 03:15 \x1b[1;34mbackups\x1b[0m\r\n" +
		"-rwxr-xr-x  1 deploy deploy  612 Jul 15 10:33 \x1b[1;32mdeploy.sh\x1b[0m\r\n" +
		"-rw-r--r--  1 deploy deploy  487 Jul 18 14:07 docker-compose.yml\r\n" +
		"drwxr-xr-x  2 deploy deploy 4096 Jul 21 09:13 \x1b[1;34mlogs\x1b[0m\r\n" +
		"-rw-r--r--  1 deploy deploy  198 Jul 20 08:55 notes.txt\r\n" +
		"-rw-r--r--  1 deploy deploy  392 Jul 13 19:26 README.md\r\n",

	"uptime": " 09:14:22 up 37 days,  4:12,  1 user,  load average: 0.18, 0.24, 0.21\r\n",

	"whoami": demoUser + "\r\n",

	"hostname": demoHost + "\r\n",

	"systemctl status caddy": "\x1b[1;32m●\x1b[0m caddy.service - Caddy web server\r\n" +
		"     Loaded: loaded (/lib/systemd/system/caddy.service; enabled; preset: enabled)\r\n" +
		"     Active: \x1b[1;32mactive (running)\x1b[0m since Fri 2026-06-13 22:41:08 UTC; 37 days ago\r\n" +
		"       Docs: https://caddyserver.com/docs/\r\n" +
		"   Main PID: 812 (caddy)\r\n" +
		"      Tasks: 9 (limit: 4657)\r\n" +
		"     Memory: 41.7M (peak: 58.2M)\r\n" +
		"        CPU: 2h 14min 8.902s\r\n" +
		"     CGroup: /system.slice/caddy.service\r\n" +
		"             └─812 /usr/bin/caddy run --environ --config /etc/caddy/Caddyfile\r\n",

	"docker compose ps": "NAME        IMAGE                          STATUS         PORTS\r\n" +
		"web         ghcr.io/example/web:1.8.2      Up 2 hours     127.0.0.1:8080->8080/tcp\r\n" +
		"db          postgres:16-alpine             Up 37 days     5432/tcp\r\n" +
		"cache       redis:7-alpine                 Up 37 days     6379/tcp\r\n",

	"docker ps": "CONTAINER ID   IMAGE                       STATUS         NAMES\r\n" +
		"3f9a1c4e77b2   ghcr.io/example/web:1.8.2   Up 2 hours     web\r\n" +
		"c81d55e0a934   postgres:16-alpine          Up 37 days     db\r\n" +
		"7ae4b0f1d6c8   redis:7-alpine              Up 37 days     cache\r\n",

	"df -h": "Filesystem      Size  Used Avail Use% Mounted on\r\n" +
		"/dev/vda1        78G   32G   43G  43% /\r\n" +
		"tmpfs           2.0G     0  2.0G   0% /dev/shm\r\n" +
		"/dev/vdb1       196G   88G  100G  47% /srv/backups\r\n",

	"free -h": "               total        used        free      shared  buff/cache   available\r\n" +
		"Mem:           3.8Gi       1.4Gi       412Mi        58Mi       2.1Gi       2.3Gi\r\n" +
		"Swap:          2.0Gi       128Mi       1.9Gi\r\n",

	"tail -n 5 logs/access.log": accessLogCRLF,

	"./deploy.sh 1.8.3": "==> pulling 1.8.3\r\n" +
		"1.8.3: Pulling from example/web\r\n" +
		"\x1b[1;32mDigest\x1b[0m: sha256:4f1c9d0ab73e5c2188f0a6d4e7b5c31a\r\n" +
		"==> rolling web\r\n" +
		" \x1b[1;32m✔\x1b[0m Container web  Started\r\n" +
		"==> healthy after 3s\r\n",
}

// accessLogCRLF is accessLog with the CRLF line endings a pty needs.
var accessLogCRLF = crlf(accessLog)

func crlf(s string) string {
	out := make([]byte, 0, len(s)+16)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, '\r')
		}
		out = append(out, s[i])
	}
	return string(out)
}
