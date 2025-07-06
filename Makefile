all:
	go build -o cutbyte cmd/cutbyte/cutbyte.go

run:
	./cutbyte

clean:
	rm -f cutbyte