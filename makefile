KEY:=BDMzY2Q2OWZlNzE0MTQ3ZmU4NTE1NTU5ZWRjMGQyNmJkAAAAAAAAAAAEAAAAAAAAAAwwNQIZAI9PK6ZJaNpualdT/iEfnz/CitRFwLArwAIYF6aW2DHP6GQXUCEE32wPwVrtTNcMLW7uAA==
ROOT_PASSWORD=RootPass1

test:
	go get "github.com/orcaman/concurrent-map"
	docker-compose up
	docker-compose -f docker-compose.yaml start
#	go test
#	docker-compose -f docker-compose.yaml stop




#	docker pull memsql/cluster-in-a-box
#	docker stop memsql-ciab && docker rm memsql-ciab
#	docker run -i --init --name memsql-ciab -e LICENSE_KEY=${KEY} -e ROOT_PASSWORD=${ROOT_PASSWORD} -p 3306:3306 -p 8080:8080 memsql/cluster-in-a-box
#	docker start memsql-ciab


