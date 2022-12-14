package kube

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	migrationNamespace      = "pv-migrate"
	migrationServiceAccount = "pv-migrate-edit-account"
)

func (cw *ClientWrapper) CreateMigrationNamespaceAndServiceAccount() error {
	err := cw.CreateNamespace(migrationNamespace)
	if err != nil {
		return err
	}

	return cw.CreateServiceAccount(migrationNamespace, migrationServiceAccount)
}

func (cw *ClientWrapper) CleanupMigrationObjects() error {
	err := cw.DeleteNamespace(migrationNamespace)
	if err != nil {
		return err
	}

	return cw.DeleteCRB(migrationServiceAccount)
}

// TODO need -d on second write? https://github.com/utkuozdemir/pv-migrate/blob/master/USAGE.md
func (cw *ClientWrapper) MigrateJob(namespace, fromPVC, toPVC string) (string, error) {
	var backOffLimit int32 = 0

	jobSpec := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			GenerateName: "pv-migrater-",
			Namespace:    migrationNamespace,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:    "pv-migrater-job-container",
							Image:   "utkuozdemir/pv-migrate:v1.0.0",
							Command: []string{"pv-migrate"},
							Args:    []string{"migrate", fromPVC, toPVC, "-n", namespace, "-N", namespace},
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: migrationServiceAccount,
				},
			},
			BackoffLimit: &backOffLimit,
		},
	}

	return cw.CreateJob(migrationNamespace, jobSpec)
}
