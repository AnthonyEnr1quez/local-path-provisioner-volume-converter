# local-path-provisioner-volume-converter

A utility to convert volumes provisioned by the [local-path-provisioner](https://github.com/rancher/local-path-provisioner) from [hostPath](https://kubernetes.io/docs/concepts/storage/volumes/#hostpath) to [local](https://kubernetes.io/docs/concepts/storage/volumes/#local).

Once the volume is converted to local type, restic backups using [Velero](https://velero.io/) are possible.

Currently supports converting volumes from the following resources:
- [Flux HelmRelease](https://fluxcd.io/flux/components/helm/helmreleases/)
- [Rancher HelmChart](https://docs.k3s.io/helm#using-the-helm-crd)

This tool was built to update pvc's using [bjw-s app-template](https://github.com/bjw-s/helm-charts/tree/main/charts/other/app-template) helm chart, so compatability with other helm charts is unlikely.
