# Управление конфигурацией серверов — MVP

Сдача: публичный gitlab.com + этот README.


Схема как у Salt: `nodes/hosts` → `roles` → `top` (вместо salt-ssh — Ansible).

---

## Идея

Ось (`roles/base`) накатываем редко. Приложения (`roles/ping_pong`) — отдельно и чаще.  
Что стоит на хосте — в `nodes/hosts/<имя>.yml`.

---

## Requirements

- Docker + Docker Compose
- Ansible ≥ 2.14
- Go ≥ 1.22

---

## Install

```bash
git clone <gitlab-url>
cd ffinfrastructure-submit
pip install -r requirements.txt
```

---

## Configure

Правь `nodes/hosts/host-a.yml` (тип машины, `host_roles`, `ff_services`).  
`inventory/hosts.yml` — только как достучаться до контейнера.

---

## Run

```bash
docker compose up -d --build
make build-linux
make base-local
make services-local
docker compose exec host-a tail -f /var/log/ff/log_ping.txt
```

Ждём строку `Statistic window … rtt_us …`.

---

## Структура

```
nodes/hosts/      # описание хоста (железо + роли + сервисы)
roles/base/       # базовая часть (ось)
roles/ping_pong/  # сервисная часть (ping-pong)
top/base.yml      # накат только base
top/services.yml  # накат только сервисов
inventory/        # коннект
cmd/ping_pong_model/
```
