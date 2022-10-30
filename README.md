# local-path-provisioner-volume-converter

A utility to convert volumes provisioned by the [local-path-provisioner](https://github.com/rancher/local-path-provisioner) from [hostPath](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath) to [local](https://kubernetes.io/docs/concepts/storage/volumes/#local).

Once the volume is converted to local type, it should be CSI compliant, thus allowing backups using [Velero](https://velero.io/).

Currently supports converting volumes from the following resources:
- [Flux HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/)
- [Rancher HelmChart](https://docs.k3s.io/helm#using-the-helm-crd)
