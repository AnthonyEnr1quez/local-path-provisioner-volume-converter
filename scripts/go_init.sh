#!/usr/bin/env bash

## init namespace
kubectl create namespace test
## check namespace created
while ! kubectl get namespace test; do :; done

## init sonarr
kubectl create -f migrate-to-local-volume-pv/00-init-home-chart.yaml
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done