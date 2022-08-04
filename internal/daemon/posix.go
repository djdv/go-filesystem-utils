package daemon

import "github.com/hugelgupf/p9/p9"

const (
	S_IROTH p9.FileMode = p9.Read
	S_IWOTH             = p9.Write
	S_IXOTH             = p9.Exec

	S_IRGRP = S_IROTH << 3
	S_IWGRP = S_IWOTH << 3
	S_IXGRP = S_IXOTH << 3

	S_IRUSR = S_IRGRP << 3
	S_IWUSR = S_IWGRP << 3
	S_IXUSR = S_IXGRP << 3

	S_IRWXO = S_IROTH | S_IWOTH | S_IXOTH
	S_IRWXG = S_IRGRP | S_IWGRP | S_IXGRP
	S_IRWXU = S_IRUSR | S_IWUSR | S_IXUSR

	// Non-standard.

	S_IRWXA = S_IRWXU | S_IRWXG | S_IRWXO              // 0777
	S_IRXA  = S_IRWXA &^ (S_IWUSR | S_IWGRP | S_IWOTH) // 0555
)
