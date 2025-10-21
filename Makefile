include .env
export

migrate-up:
	migrate -path db/migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DATABASE_URL)" down -all

migrate-drop:
	migrate -path db/migrations -database "$(DATABASE_URL)" drop -f

migrate-version:
	migrate -path db/migrations -database "$(DATABASE_URL)" version

seed:
	psql "$(DATABASE_URL)" -f db/seeds/dev_seed.sql

reset:
	make migrate-drop
	make migrate-up
	make seed	