#!/bin/bash

## init namespace
kubectl create namespace test
## check namespace created
while ! kubectl get namespace test; do :; done

## init sonarr
kubectl create -f migrate-to-local-volume-pv/00-init-home-chart.yaml
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## add extra file to config dir
kubectl exec -n test deploy/sonarr -- sh -c 'echo "Hello World!" > /config/hello.txt'
if kubectl exec -n test deploy/sonarr -- sh -c 'cat /config/hello.txt' | grep -q 'Hello World!'; then
    echo "file persisted in pv"
fi

##add temp local pv to sonarr
kubectl patch -n kube-system helmchart sonarr --patch '{"spec": {"valuesContent": "persistence:\n  config:\n    enabled: true\n    retain: true\n  temp:\n    enabled: true\n    retain: true\n    accessMode: ReadWriteOnce\n    size: 1Gi\n    annotations:\n      volumeType: local"}}' --type=merge
while ! kubectl get -n test pvc sonarr-temp; do :; done
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## scale deployment to 0
kubectl scale -n test deployment sonarr --replicas=0
while [ $(kubectl get -n test deployment sonarr -o jsonpath='{.spec.replicas}') -ne 0 ]; do :; done
while kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## pv migrate
pv-migrate migrate sonarr-config sonarr-temp -n test -N test

## delete local path pvc
kubectl delete -n test pvc sonarr-config

## check pvc is deleted
while kubectl get -n test pvc sonarr-config; do :; done

## recreate config dir with local volume
kubectl patch -n kube-system helmchart sonarr --patch '{"spec": {"valuesContent": "persistence:\n  config:\n    enabled: true\n    retain: true\n    annotations:\n      volumeType: local\n  temp:\n    enabled: true\n    retain: true\n    accessMode: ReadWriteOnce\n    size: 1Gi\n    annotations:\n      volumeType: local"}}' --type=merge
while ! kubectl get -n test pvc sonarr-config; do :; done
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## add extra file to config dir
kubectl exec -n test deploy/sonarr -- sh -c 'echo "I shouldnt be here!" > /config/dont-want.txt'
kubectl exec -n test deploy/sonarr -- sh -c 'echo "I shouldnt be here!" > /config/hello.txt'
if kubectl exec -n test deploy/sonarr -- sh -c 'cat /config/dont-want.txt' | grep -q 'I shouldnt be here!'; then
    echo "file persisted in pv"
fi

## scale deployment to 0
kubectl scale -n test deployment sonarr --replicas=0
while [ $(kubectl get -n test deployment sonarr -o jsonpath='{.spec.replicas}') -ne 0 ]; do :; done
while kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

# pv migrate
pv-migrate migrate sonarr-temp sonarr-config -d -n test -N test

## remove temp volume
kubectl patch -n kube-system helmchart sonarr --patch '{"spec": {"valuesContent": "persistence:\n  config:\n    enabled: true\n    retain: true\n    annotations:\n      volumeType: local"}}' --type=merge
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## delete temp pvc
kubectl delete -n test pvc sonarr-temp

## check pvc is deleted
while kubectl get -n test pv sonarr-temp; do :; done

## check the files
if kubectl exec -n test deploy/sonarr -- sh -c 'cat /config/hello.txt' | grep -q 'Hello World!'; then
    echo "file moved to correct in pv"
fi

if kubectl exec -n test deploy/sonarr -- sh -c 'test -f /config/dont-want.txt'; then
    echo "extra file here"
fi
