#!/usr/bin/env sh
# Render every Kubernetes manifest with placeholder values and validate it
# against the Kubernetes schemas. Offline: no cluster, no credentials.
#
# Nothing checked these before, and nothing had ever applied them. The deploy
# script referenced three manifests that did not exist, the web and ingress were
# never applied, and a service address named a namespace the script never used.
# Schema validation would not have caught all of that, but it catches the class
# of mistake that turns a first real deploy into an afternoon of kubectl errors.
set -eu

out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

for f in deploy/k8s/*.yaml; do
	sed -e 's|{{JOB_NAME}}|jobtrack-migrate-check|g' -e 's|{{VERSION}}|0.0.0-check|g' \
		-e 's|{{REGISTRY}}|ghcr.io/ergodicregulus/jobtrack|g' \
		-e 's|{{HOST}}|jobs.example.com|g' -e 's|{{POD_CIDR}}|10.244.0.0/16|g' \
		"$f" >"$out/$(basename "$f")"
done

if grep -l '{{' "$out"/*.yaml; then
	echo "unfilled placeholder in the manifests above: add it to deploy.sh render()" >&2
	exit 1
fi

docker run --rm -v "$out:/m:ro" ghcr.io/yannh/kubeconform:v0.6.7 \
	-strict -summary -kubernetes-version 1.31.0 /m
