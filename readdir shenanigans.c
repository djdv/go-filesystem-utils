#include <stdio.h>
#include <stdlib.h>
#include <dirent.h>

#define pathBufferSize 1024
#define markerToStore 2


void readdirPath(const char*);

void main(void) {
	char pathTarg[pathBufferSize] = "";
	{
		char *pPtr;
		if (pPtr = getenv("TEST_PATH_TARGET")) {
			strncpy(pathTarg, pPtr, pathBufferSize - 1);
		} else {
			pathTarg[0] = '.'; // XXX never do this in a real program
			pathTarg[1] = '\0';
		}
	}

	printf("Target walk: (%s)...\n", pathTarg);
	getchar();
	readdirPath(pathTarg);
}

void readdirPath(const char* path) {
	DIR *dir;
	struct dirent *ptr;
	int offset, storedOffset = 0;
	dir = opendir(path);

	for (int i = 0; (ptr = readdir(dir));) {
		offset = telldir(dir);
		printf("Name:\"%s\"\toffset:%d\n", ptr->d_name, offset);
		if (++i == markerToStore) {
		printf("Storing offset %d for later\n", offset);
			storedOffset = offset;
		}
	}

	printf("Seek back to %d and readdir again...\n", storedOffset);
	getchar();
	seekdir(dir, storedOffset);
	while ((ptr = readdir(dir)) != NULL) {
		offset = telldir(dir);
		printf("Name:\"%s\"\toffset:%d\n", ptr->d_name, offset);
	}

	printf("rewinddir...");
	getchar();
	rewinddir(dir);
	printf("seekdir to %d...", storedOffset);
	getchar();
	seekdir(dir, storedOffset);
	printf("Readdir again with offset after rewinddir (unspecified behaviour (SUS))...\n");
	getchar();
	while ((ptr = readdir(dir)) != NULL) {
		offset = telldir(dir);
		printf("Name:\"%s\"\toffset:%d\n", ptr->d_name, offset);
	}

	closedir(dir);
}
