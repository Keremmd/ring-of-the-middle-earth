.PHONY: up down test build logs clean help

COMPOSE = docker compose -f docker-compose.yml

help:
	@echo "Ring of the Middle Earth — Make targets"
	@echo ""
	@echo "  make up       Start the entire system"
	@echo "  make down     Stop all services"
	@echo "  make test     Run all unit tests (no Docker required)"
	@echo "  make build    Build Go binary"
	@echo "  make logs     Tail all service logs"
	@echo "  make clean    Remove containers and volumes"

up:
	$(COMPOSE) up --build -d
	@echo "System starting. Light Side: http://localhost?playerId=light"
	@echo "Dark Side: http://localhost?playerId=dark"
	@echo "Schema Registry: http://localhost:8081"

down:
	$(COMPOSE) down

test:
	cd option-b && go test ./... -v -race

build:
	cd option-b && CGO_ENABLED=1 go build -o bin/server ./cmd/server/

logs:
	$(COMPOSE) logs -f

clean:
	$(COMPOSE) down -v --remove-orphans
	rm -f option-b/bin/server

topics:
	docker exec kafka-1 kafka-topics --bootstrap-server kafka-1:9092 --list

describe-topics:
	docker exec kafka-1 kafka-topics --bootstrap-server kafka-1:9092 --describe

watch-broadcast:
	docker exec kafka-1 kafka-console-consumer \
		--bootstrap-server kafka-1:9092 \
		--topic game.broadcast \
		--from-beginning

watch-dlq:
	docker exec kafka-1 kafka-console-consumer \
		--bootstrap-server kafka-1:9092 \
		--topic game.dlq \
		--from-beginning

kill-go2:
	docker stop go-2

start-go2:
	docker start go-2
