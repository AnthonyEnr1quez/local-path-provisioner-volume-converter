create-cluster:
	k3d cluster create mycluster
	kubectl -n kube-system set image deployment/local-path-provisioner local-path-provisioner=rancher/local-path-provisioner:v0.0.23

delete-cluster:
	k3d cluster delete mycluster
