test:
	make start-machine

#	docker-compose down


delete-container:
	docker-compose down
	docker image rm -f memsql/cluster-in-a-box

start-machine:
	docker-compose up --force-recreate --detach

stop-machine:
	docker-compose down

run-tests:
	go get "github.com/orcaman/concurrent-map"
	go get "github.com/stretchr/testify/assert"
	#go test