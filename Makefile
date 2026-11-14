BINARY = data-collection

.PHONY: build run demo backup clean service

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

demo: build
	./$(BINARY) -demo -db /tmp/pendataan-demo.db

service:
	./deploy/install-service.sh

backup:
	./deploy/backup.sh

clean:
	rm -f $(BINARY)
