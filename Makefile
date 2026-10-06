include .env
export

export PROJECT_ROOT=${shell pwd}

env-up:
	@docker compose up -d golang-kp-postgres

env-down:
	@docker compose down golang-kp-postgres

env-cleanup:
	@read -p "Очистить все volume файлы окружения? Опасность утери данных. [Y/N]: " ans; \
	if [ "$$ans" = "Y" ]; then \
	  make env-down  && \
	  make env-port-close && \
	  rm -rf ${PROJECT_ROOT}/out/pgdata && \
	  echo "Файлы окружения успешно очищены"; \
	else \
	  echo "Очистка окружения отменена"; \
	fi

env-port-forwarder:
	@docker compose up -d port-forwarder

env-port-close:
	@docker compose down port-forwarder

migrate-create:
	@if [ -z "$(seq)" ]; then \
	    echo "Отсутствует обязательный параметр seq. Пример использования make migrate-create seq=init"; \
	    exit 1; \
	fi; \
	docker compose run --rm golang-kp-migrate \
		create \
		-ext sql \
		-dir /migrations \
		-seq "$(seq)"

migrate-up:
	@make migrate-action action=up n=$(n)

migrate-down:
	@make migrate-action action=down n=$(if $(n),$(n),1)

migrate-ensure-schema:
	@docker compose exec -T golang-kp-postgres \
		psql -U ${POSTGRES_USER} -d ${POSTGRES_DB} -v ON_ERROR_STOP=1 \
		-c "CREATE SCHEMA IF NOT EXISTS golang_kp;" > /dev/null

migrate-action:
	@if [ -z "$(action)" ]; then \
	    echo "Отсутствует обязательный параметр action."; \
		exit 1; \
	fi; \
	make migrate-ensure-schema && \
	docker compose run --rm golang-kp-migrate \
		-path /migrations \
		-database "postgres://${POSTGRES_USER}:${POSTGRES_PASSWORD}@golang-kp-postgres:5432/${POSTGRES_DB}?sslmode=disable&search_path=golang_kp" \
		$(action) $(n)

app-run:
	@export LOGGER_FOLDER=${PROJECT_ROOT}/out/logs && \
	export POSTGRES_HOST=localhost && \
	go mod tidy && \
	go run ${PROJECT_ROOT}/cmd/golang-kp/main.go