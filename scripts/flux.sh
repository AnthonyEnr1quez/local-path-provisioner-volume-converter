#!/usr/bin/env bash

## https://fluxcd.io/flux/installation/#dev-install
flux install

## init namespace
kubectl create namespace test
## check namespace created
while ! kubectl get namespace test; do :; done

## init helm charts repo
kubectl create -f yaml/flux/charts.yaml

## init sonarr
kubectl create -f yaml/flux/helm-release.yaml
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## init radarr
kubectl create -f yaml/radarr.yaml
while ! kubectl wait pods -n test -l app.kubernetes.io/name=radarr --for condition=Ready --timeout=90s; do sleep 1; done
