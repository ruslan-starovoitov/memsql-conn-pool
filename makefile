KEY:=DMzY2Q2OWZlNzE0MTQ3ZmU4NTE1NTU5ZWRjMGQyNmJkAAAAAAAAAAAEAAAAAAAAAAwwNQIZAI9PK6ZJaNpualdT/iEfnz/CitRFwLArwAIYF6aW2DHP6GQXUCEE32wPwVrtTNcMLW7uAA==
NAME=hellomemsql
test:
	go get github.com/hashicorp/golang-lru
#	docker-compose -f docker-compose.yaml stop
	export LICENSE_KEY=${KEY}
	export TEST_DATABASE_NAME=${NAME}
	docker-compose -f docker-compose.yaml start
#	go test
#	docker-compose -f docker-compose.yaml stop


