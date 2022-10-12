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

## delete helm chart
kubectl delete -f migrate-to-local-volume-pv/00-init-home-chart.yaml

## check chart is deleted
while kubectl get -n test deployment sonarr; do :; done

## add temp local pv to sonarr
kubectl create -f migrate-to-local-volume-pv/01-add-temp-local-pv.yaml
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## scale deployment to 0
kubectl scale -n test deployment sonarr --replicas=0
while [ $(kubectl get -n test deployment sonarr -o jsonpath='{.spec.replicas}') -ne 0 ]; do :; done
while kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## pv migrate
pv-migrate migrate sonarr-config sonarr-temp -n test -N test

## delete helm chart
kubectl delete -f migrate-to-local-volume-pv/01-add-temp-local-pv.yaml

## check chart is deleted
while kubectl get -n test deployment sonarr; do :; done

## delete local path pvc
kubectl delete -n test pvc sonarr-config

## check pvc is deleted
while kubectl get -n test pvc sonarr-config; do :; done

## recreate config dir with local volume
kubectl create -f migrate-to-local-volume-pv/02-config-local-pv.yaml
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

## scale deployment to 1
kubectl scale -n test deployment sonarr --replicas=1
while [ $(kubectl get -n test deployment sonarr -o jsonpath='{.spec.replicas}') -ne 1 ]; do :; done

## delete helm chart
kubectl delete -f migrate-to-local-volume-pv/02-config-local-pv.yaml

## check chart is deleted
while kubectl -n test get deployment sonarr; do :; done

## delete temp pvc
kubectl delete -n test pvc sonarr-temp

## check pvc is deleted
while kubectl get -n test pvc sonarr-temp; do :; done

## remove temp volume
kubectl create -f migrate-to-local-volume-pv/03-remove-temp-local-pv.yaml
while ! kubectl wait pods -n test -l app.kubernetes.io/name=sonarr --for condition=Ready --timeout=90s; do sleep 1; done

## check the files
if kubectl exec -n test deploy/sonarr -- sh -c 'cat /config/hello.txt' | grep -q 'Hello World!'; then
    echo "file moved to correct in pv"
fi

if kubectl exec -n test deploy/sonarr -- sh -c 'test -f /config/dont-want.txt'; then
    echo "extra file here"
fi
