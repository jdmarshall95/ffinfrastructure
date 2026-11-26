# Управление конфигурацией серверов — MVP

## Requirements

- Docker + Docker Compose
- Ansible ≥ 2.14
- Go ≥ 1.22

Опционально, две ВМ в Yandex Cloud: [extras/](extras/).
## Install

```bash
git clone https://github.com/jdmarshall95/ffinfrastructure.git
cd ffinfrastructure
pip install -r requirements.txt
```
## Configure

`nodes/hosts/host-a.yml` тип машины, `host_roles`, `ff_services`
`inventory/hosts.yml` на какие узлы применяем
## Run

Ansible запускается на своей машине и заходит в контейнер `host-a` через Docker. Контейнер сам ничего не настраивает.

пустая Ubuntu 24.04
```bash
docker compose up -d
```

Бинарь лежит в `bin/` и в git не входит. Playbook сервисов копирует его в контейнер. Для проверки собран свой вариант: тот же запуск, что у бинаря с прошлого этапа.

```bash
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ping_pong_model ./cmd/ping_pong_model
```

Их бинарь кладётся на то же место, без сборки:

```bash
cp /path/to/binary bin/ping_pong_model
```

sysctl, IRQ
```bash
ansible-playbook -i inventory/hosts.yml top/base.yml
```

Cервисы: бинарь, два JSON, pong, потом ping. Общий файл /dev/shm/ff-ping-pong
```bash
ansible-playbook -i inventory/hosts.yml top/services.yml
```
В логе ждём Statistic window … rtt_us …
```bash
docker compose exec host-a tail -f /var/log/ff/log_ping.txt
```
## Сценарии масштабирования

Машины создаем руками или через terraform. Сначала появляется сервер (в примере это контейнер), потом файл в `nodes/hosts/` и запись в inventory. Имя файла и имя хоста совпадают.

Бинарь работает в 2 режимах: pong пишет в shared memory, ping читает. Несколько пар на одной машине — у каждой свой `shm_path` и свои `pin_slot`. `pin_slot` — номер в списке `ff_isolated_cores`.

### Пусто, нужна одна машина на 8 ядер и 6 процессов

Одно ядро системе, одно общее для пар, шесть — трём парам.

`inventory/hosts.yml` — как зайти на `trade-1`.  
`nodes/hosts/trade-1.yml`:

```yaml
cpu_vendor: intel
hyper_threading: true
ff_hw_concurrency: 8
ff_os_cores: [0, 1]
ff_isolated_cores: [2, 3, 4, 5, 6, 7]

host_roles:
  - base
  - ping_pong

ff_services:
  - { name: pong-a, role: pong, pin_slot: 0, shm_path: /dev/shm/ff-a }
  - { name: ping-a, role: ping, pin_slot: 1, shm_path: /dev/shm/ff-a }
  - { name: pong-b, role: pong, pin_slot: 2, shm_path: /dev/shm/ff-b }
  - { name: ping-b, role: ping, pin_slot: 3, shm_path: /dev/shm/ff-b }
  - { name: pong-c, role: pong, pin_slot: 4, shm_path: /dev/shm/ff-c }
  - { name: ping-c, role: ping, pin_slot: 5, shm_path: /dev/shm/ff-c }
```

Слот 0 берёт ядро 2, маска при длине 8 — `00100000`. Слот 5 берёт ядро 7. Пара `a` делит `/dev/shm/ff-a`, остальные пары свои файлы.

Бинарь в `bin/ping_pong_model`, дальше только эта машина:

```bash
ansible-playbook -i inventory/hosts.yml top/base.yml --limit trade-1
ansible-playbook -i inventory/hosts.yml top/services.yml --limit trade-1
```

Сначала основная конфигурация, потом процессы. В примере в репозитории одна пара на `host-a`: конфиг и pid называются по `role` (`config_pong.json`, `ff-pong.pid`). Второй pong на той же машине затрёт первого. Для шести процессов имя файла и pid надо брать из `name`.

### Одна машина уже работает, нужны ещё две

Файл `trade-1` не меняется, `top/base.yml` на ней снова не запускается.

Две пустые машины, две строки в inventory, `nodes/hosts/trade-2.yml` и `trade-3.yml`. Если процессор другой, в файле другие `cpu_vendor`, `hyper_threading` и список ядер. Список процессов может быть тем же.
```bash
ansible-playbook -i inventory/hosts.yml top/base.yml --limit trade-2,trade-3
ansible-playbook -i inventory/hosts.yml top/services.yml --limit trade-2,trade-3
```

`trade-1` в прогон не входит, его процессы не перезапускаются.
Два новых процесса на старой машине — это не base. В её `ff_services` дописывается пара со свободными `pin_slot` и своим `shm_path`, затем только `top/services.yml --limit trade-1`.

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
.gitlab-ci.yml    # те же два playbook, руками
```

## GitLab CI и Terraform

К локальному прогону это не нужно. В задании Terraform нет.

`.gitlab-ci.yml` повторяет те же два шага, что и руками. Оба job с `when: manual`. Job `base` запускает `top/base.yml`. Job `services` сначала собирает `bin/ping_pong_model`, потом запускает `top/services.yml`. Inventory в job — `inventory/hosts.yml`, то есть контейнер `host-a` на машине, где крутится runner (в реальности рпедполагается, что у раннера есть доступ до машин и он выполняет плейбуки уже на них)

`extras/terraform/` поднимает две ВМ в Yandex Cloud: `host-intel-ht` (`standard-v3`) и `host-amd-noht` (AMD Zen 4, `standard-v4a`, 2 vCPU, 20%, 1 ГБ). Описание машины по-прежнему в `nodes/hosts/<имя>.yml`. Terraform пишет только способ зайти, в `extras/inventory/yc.hosts.yml` (файл в gitignore). Дальше те же playbook:

```bash
cd extras/terraform
cp terraform.tfvars.example terraform.tfvars   # cloud_id, folder_id, subnet_id
terraform init && terraform apply

cd ../..
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ping_pong_model ./cmd/ping_pong_model
ansible-playbook -i extras/inventory/yc.hosts.yml top/base.yml
ansible-playbook -i extras/inventory/yc.hosts.yml top/services.yml
```

Платформы `standard-v4a` нет в зонах `ru-central1-d` и `ru-central1-e`. В примере зона `ru-central1-a`.
