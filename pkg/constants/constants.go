package constants

const (
	VELERO_BACKUP_ANNOTATION_KEY              = "backup.velero.io/backup-volumes"
	VELERO_BACKUP_EXCLUDES_ANNOTATION_KEY     = "backup.velero.io/backup-volumes-excludes"
	VOLUME_BACKUP_INCLUDE_ANNOTATION_KEY      = "vvc.smoothify.com/backup-volume"
	VOLUME_BACKUP_EXCLUDE_ANNOTATION_KEY      = "vvc.smoothify.com/backup-volume-exclude"
	POD_BACKUP_MANAGED_ANNOTATION_KEY         = "vvc.smoothify.com/backup-volumes-managed"
)
