DB_URL=postgres://sales_assistant_app:sales_assistant_app@localhost:5432/sales_assistant_db?sslmode=disable

migrate-up:
	migrate -path db/migrations -database "$(DB_URL)" up

migrate-down:
	migrate -path db/migrations -database "$(DB_URL)" down -all

migrate-drop:
	migrate -path db/migrations -database "$(DB_URL)" drop -f

seed:
	psql "$(DB_URL)" -f db/seeds/dev_seed.sql

reset:
	make migrate-drop
	make migrate-up
	make seed	