BINARY = data-collection

.PHONY: build run demo backup clean

build:
	go build -o $(BINARY) .

run: build
	./$(BINARY)

demo: build
	./$(BINARY) -demo -db /tmp/pendataan-demo.db

backup:
	./deploy/backup.sh

clean:
	rm -f $(BINARY)
