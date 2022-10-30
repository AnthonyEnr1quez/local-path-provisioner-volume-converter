basic-backup-flow:
	make create-cluster
	make init-velero
	./basic-backup-flow.sh
	make delete-cluster

.PHONY: create-cluster
create-cluster:
	k3d cluster create mycluster
	kubectl -n kube-system set image deployment/local-path-provisioner local-path-provisioner=rancher/local-path-provisioner:v0.0.23

.PHONY: init-velero
init-velero:
	kubectl create -f https://raw.githubusercontent.com/vmware-tanzu/velero/main/examples/minio/00-minio-deployment.yaml
	while ! kubectl wait pods -n velero -l component=minio --for condition=Ready --timeout=90s; do sleep 1; done
	kubectl create -f velero.yaml
	while ! kubectl wait pods -n velero -l name=restic --for condition=Ready --timeout=90s; do sleep 1; done
	while ! kubectl wait pods -n velero -l name=velero --for condition=Ready --timeout=90s; do sleep 1; done

.PHONY: delete-cluster
delete-cluster:
	k3d cluster delete mycluster
