KEY:=BDMzY2Q2OWZlNzE0MTQ3ZmU4NTE1NTU5ZWRjMGQyNmJkAAAAAAAAAAAEAAAAAAAAAAwwNQIZAI9PK6ZJaNpualdT/iEfnz/CitRFwLArwAIYF6aW2DHP6GQXUCEE32wPwVrtTNcMLW7uAA==
NAME=hellomemsql

test:
#"github.com/orcaman/concurrent-map"
#	docker pull memsql/cluster-in-a-box
#	docker-compose -f docker-compose.yaml stop
	export LICENSE_KEY=${KEY}
	export TEST_DATABASE_NAME=${NAME}
#	docker-compose-up
#	docker run memsql/cluster-in-a-box
	docker-compose -f docker-compose.yaml start
#	go test
#	docker-compose -f docker-compose.yaml stop


