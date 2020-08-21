package constants

const (
	VELERO_BACKUP_ANNOTATION_KEY         = "backup.velero.io/backup-volumes"
	VOLUME_TYPE_PERSISTENTVOLUMECLAIM    = "persistentVolumeClaim"
	VOLUME_BACKUP_INCLUDE_ANNOTATION_KEY = "vvc.velero.io/backup-volume"
	VOLUME_BACKUP_EXCLUDE_ANNOTATION_KEY = "vvc.velero.io/backup-volume-exclude"
)
