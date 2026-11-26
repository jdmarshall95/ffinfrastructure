.PHONY: build-linux up-local base-local services-local yc-init yc-apply yc-base yc-services yc-destroy

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ping_pong_model_linux ./cmd/ping_pong_model

up-local:
	docker compose up -d --build

base-local:
	ansible-playbook -i inventory/hosts.yml top/base.yml

services-local: build-linux
	ansible-playbook -i inventory/hosts.yml top/services.yml

yc-init:
	cd extras/terraform && terraform init

yc-apply: build-linux
	cd extras/terraform && terraform apply

yc-base:
	ansible-playbook -i extras/inventory/yc.hosts.yml top/base.yml

yc-services: build-linux
	ansible-playbook -i extras/inventory/yc.hosts.yml top/services.yml

yc-destroy:
	cd extras/terraform && terraform destroy
