OUT_DIR = build
PACKAGE = k8s.io/custom-metrics-boilerplate
PREFIX = gcr.io/kawych-test
TAG = 1.0

PKG := $(shell find pkg/* -type f)

deps:
	glide install --strip-vendor

build/sample: sample-main.go $(PKG)
	go build -a -o $(OUT_DIR)/sample sample-main.go

docker: build/sample
	docker build --pull -t ${PREFIX}/custom-metrics-boilerplate:$(TAG) .

push: docker
	gcloud docker -- push ${PREFIX}/custom-metrics-boilerplate:$(TAG)

clean:
	rm -rf build
