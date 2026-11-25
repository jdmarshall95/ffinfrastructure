.PHONY: build-linux up-local base-local services-local

build-linux:
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/ping_pong_model_linux ./cmd/ping_pong_model

up-local:
	docker compose up -d --build

base-local:
	ansible-playbook -i inventory/hosts.yml top/base.yml

services-local: build-linux
	ansible-playbook -i inventory/hosts.yml top/services.yml
