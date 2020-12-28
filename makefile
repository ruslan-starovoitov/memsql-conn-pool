# LICENSE_KEY_TEST=BDMzY2Q2OWZlNzE0MTQ3ZmU4NTE1NTU5ZWRjMGQyNmJkAAAAAAAAAAAEAAAAAAAAAAwwNQIZAI9PK6ZJaNpualdT/iEfnz/CitRFwLArwAIYF6aW2DHP6GQXUCEE32wPwVrtTNcMLW7uAA==
# ROOT_PASSWORD_TEST=RootPass1

test:
	go get "github.com/orcaman/concurrent-map"
#	export LICENSE_KEY=${LICENSE_KEY_TEST}
#	export ROOT_PASSWORD=${ROOT_PASSWORD_TEST}
	docker-compose up --force-recreate --detach
#	go test
#	docker-compose down

#	docker pull memsql/cluster-in-a-box
#	docker stop memsql-ciab && docker rm memsql-ciab
#	docker run -i --init --name memsql-ciab -e LICENSE_KEY=${LICENSE_KEY} -e ROOT_PASSWORD=${ROOT_PASSWORD} -p 3306:3306 -p 8080:8080 memsql/cluster-in-a-box
#	docker start memsql-ciab




delete-container:
	docker image rm memsql/cluster-in-a-box


