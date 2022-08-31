#!/bin/bash

## init pod and pvc
kubectl create -f basic-backup-flow.yaml
while ! kubectl wait pods -n test -l name=volume-test --for condition=Ready --timeout=90s; do sleep 1; done

## add file to pv
kubectl exec -n test volume-test -- sh -c 'echo "Hello World!" > /data/hello.txt'
if kubectl exec -n test volume-test -- sh -c 'cat /data/hello.txt' | grep -q 'Hello World!'; then
    echo "file persisted in pv"
fi

## create backuip
velero backup create test-backup --include-namespaces test --wait

## remove pod and pvc
kubectl delete namespace test

## check namespace deleted
while kubectl get namespace test; do :; done

## restore namespace, pod, pvc, pv
velero restore create --from-backup test-backup --wait

## check if pv and pod was restored
while ! kubectl wait pods -n test -l name=volume-test --for condition=Ready --timeout=90s; do sleep 1; done
if kubectl exec -n test volume-test -- sh -c 'cat /data/hello.txt' | grep -q 'Hello World!'; then
    echo "backup worked!"
fi
