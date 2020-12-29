test:
	go get "github.com/orcaman/concurrent-map"
	go get "github.com/stretchr/testify/assert"
	docker-compose up --force-recreate --detach
#	go test
#	docker-compose down


delete-container:
	docker image rm memsql/cluster-in-a-box


