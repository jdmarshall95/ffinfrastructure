# Управление конфигурацией серверов — MVP

## Идея

Ось (`roles/base`) накатываем редко. Приложения (`roles/ping_pong`) — отдельно и чаще.  
Что стоит на хосте — в `nodes/hosts/<имя>.yml`.

---

## Requirements

- Docker + Docker Compose
- Ansible ≥ 2.14
- Go ≥ 1.22

Опционально, две ВМ в Yandex Cloud: [extras/](extras/).

---

## Install

```bash
git clone <gitlab-url>
cd ffinfrastructure-submit
pip install -r requirements.txt
```

---

## Configure

`nodes/hosts/host-a.yml` тип машины, `host_roles`, `ff_services`
`inventory/hosts.yml` на какие узлы применяем

---

## Run

Ansible запускается на своей машине и заходит в контейнер `host-a` через Docker. Контейнер сам ничего не настраивает.

```bash
# пустая Ubuntu 24.04
docker compose up -d

# бинарь собирается здесь, в контейнер его копирует playbook сервисов
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ping_pong_model_linux ./cmd/ping_pong_model

# sysctl, IRQ
ansible-playbook -i inventory/hosts.yml top/base.yml

# сервисы: бинарь, два JSON, pong, потом ping. Общий файл /dev/shm/ff-ping-pong
ansible-playbook -i inventory/hosts.yml top/services.yml

# в логе ждём Statistic window … rtt_us …
docker compose exec host-a tail -f /var/log/ff/log_ping.txt
```

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
extras/           # не ядро задания: Terraform, две ВМ
```
