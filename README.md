# wg-conf

WireGuard management service written in Go. Compatible with [angristan/wireguard-install](https://github.com/angristan/wireguard-install) (`/etc/wireguard/params` + `wg0.conf`).

## Features

- REST API for peer management (create, list, revoke)
- Web dashboard with online status and traffic stats
- Background monitoring (handshakes, RX/TX)
- Audit log
- QR codes for client configs

## Requirements

- Linux server with WireGuard already installed (angristan script or manual)
- Root or capabilities to run `wg` / `wg-quick`
- Go 1.22+ (for building)

## Build

```bash
cd wg-conf
go build -o wg-conf ./cmd/wg-conf
```

## Run

**Local development** (without root, uses `./dev` fixtures):

```bash
go run ./cmd/wg-conf -dev
```

**Production** (on server with angristan WireGuard):

```bash
# 1. Проверьте, что WireGuard установлен
ls -la /etc/wireguard/params /etc/wireguard/wg0.conf
systemctl status wg-quick@wg0

# 2. Соберите бинарник (быстрее и надёжнее, чем go run)
go build -o wg-conf ./cmd/wg-conf

# 3. Запуск
export WG_CONF_API_KEY="your-secret-key"
./wg-conf -listen :8080
```

По умолчанию клиентские конфиги ищутся в `/root/` (формат angristan: `wg0-client-ИМЯ.conf`). Если файлы лежат в другом месте:

```bash
./wg-conf -clients-dir /root -listen :8080
```

Ожидаемый вывод при успешном старте:

```
level=INFO msg="wg-conf starting" listen=:8080 params=/etc/wireguard/params
level=INFO msg="HTTP server listening" addr=:8080 interface=wg0 url=http://localhost:8080
```

Откройте в браузере `http://IP-СЕРВЕРА:8080`. Если порт закрыт — откройте его в firewall.

`go run` при первом запуске может молча компилировать 1–3 минуты (скачивание зависимостей). Лучше использовать `go build`.

Open `http://server:8080/` — enter API key in the UI if set.

## API

| Method | Path                       | Description                    |
| ------ | -------------------------- | ------------------------------ |
| GET    | `/api/server`              | Server info                    |
| GET    | `/api/peers`               | List peers with stats          |
| POST   | `/api/peers`               | Create peer `{"name":"alice"}` |
| GET    | `/api/peers/{name}/config` | Download client config         |
| GET    | `/api/peers/{name}/qr`     | QR code PNG                    |
| DELETE | `/api/peers/{name}`        | Revoke peer                    |
| GET    | `/api/stats`               | Aggregated traffic stats       |
| GET    | `/api/audit`               | Audit log                      |

Auth: header `X-API-Key` or `Authorization: Bearer <key>`.

## systemd example

```ini
[Unit]
Description=wg-conf WireGuard manager
After=network.target wg-quick@wg0.service

[Service]
Type=simple
Environment=WG_CONF_API_KEY=change-me
ExecStart=/usr/local/bin/wg-conf -listen 127.0.0.1:8080
Restart=on-failure

[Install]
WantedBy=multi-user.target
```

Put nginx/caddy in front for HTTPS if exposing publicly.
