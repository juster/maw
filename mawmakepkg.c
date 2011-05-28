/* mawmakepkg.c
 * A simple C program that sets our userid to the SUDO_USER username before running makepkg.
 * This only exists because I don't like building packages as root. Also sets PACMAN environment
 * variable to maw. Wraps makepkg inside bash code that will print the path names of the
 * built package files to a temporary file. The name of this file must be passed on the command
 * line to the program, preceding makepkg command arguments.
 */

#include <stdlib.h>
#include <stdio.h>
#include <string.h>
#include <unistd.h>
#include <error.h>
#include <errno.h>

#include <sys/types.h>
#include <pwd.h>

#define BASH_HACK_TEMPL \
"exit () {\n" \
"  if [ \"$1\" -ne 0 ] ; then command exit $1 ; fi\n" \
"  fullver=$(get_full_version $epoch $pkgver $pkgrel)\n" \
"  for pkg in ${pkgname[@]} ; do\n" \
"    for arch in \"$CARCH\" any ; do\n" \
"      pkgfile=\"${PKGDEST}/${pkg}-${fullver}-${arch}${PKGEXT}\"\n" \
"      if [ -f \"$pkgfile\" ] ; then\n" \
"        echo \"$pkgfile\" >> %s\n" /* <--- temp file path inserted here */ \
"      fi\n" \
"    done\n" \
"  done\n" \
"  command exit 0\n" \
"}\n" \
"source makepkg"

/* Look up user entry for the SUDO_USER. */
struct passwd *
getSudoUserInfo ()
{
    char *sudoUser;
    struct passwd *userEntry;
    
    sudoUser = getenv("SUDO_USER");
    if (sudoUser == NULL) {
        error(1, 0, "it does not appear that we are running under sudo, aborting.");
    }
    
    userEntry = getpwnam(sudoUser);
    if (userEntry == NULL) {
        error(1, 0, "the SUDO_USER named '%s' was not found.", sudoUser);
    }
    
    return userEntry;
}

/* Setup the build environment. Set user and group to our original user.
 * Sets the PACMAN environment variable to make sure makepkg uses maw to sync deps.
 */
void
setupBuildEnv()
{
    struct passwd *pwentry;
    int ret;
    
    pwentry = getSudoUserInfo();
    ret = setgid(pwentry->pw_gid);
    if (ret == -1) {
        error(1, errno, "failed to set gid to %d for %s user.", pwentry->pw_gid, pwentry->pw_name);
    }
    ret = setuid(pwentry->pw_uid);
    if (ret == -1) {
        error(1, errno, "failed to set uid to %d for %s user.", pwentry->pw_uid, pwentry->pw_name);
    }
    
    ret = setenv("PACMAN", "maw", 1);
    if (ret == -1) {
        error(1, errno, "failed to set PACMAN env. variable.");
    }
    
    return;
}

char *
bashHack(char *tmpFileName)
{
    char *bashStr;
    int hackLen;
    
    hackLen = strlen(BASH_HACK_TEMPL) - 2 + strlen(tmpFileName);
    bashStr = malloc(hackLen);
    if (bashStr == NULL) {
        error(1, errno, "failed to allocate memory for back hack.");
    }
    
    sprintf(bashStr, BASH_HACK_TEMPL, tmpFileName);
    return bashStr;
}

/* Runs makepkg by calling bash and overriding parameters. This hooks into the exit calls of
 * makepkg in order to print the paths of the built packages.
 */
void
startMakepkg (char *tmpPath, int argc, char **argv)
{
    char **args;
    int i, ret;
    
    /* Create a new array for arguments with a NULL pointer at the end.
     * When passing parameters to bash on the command line, anything after the string given to
     * -c overrides bash's $0, $1, etc parameters. We could also change $0 directly but I guess
     * this is hackish enough.
     */
    args = calloc(argc+5, sizeof(char *));
    args[0] = "bash";
    args[1] = "-c";
    args[2] = bashHack(tmpPath);
    args[3] = "makepkg";
    for (i=0; i<argc; ++i) {
        args[i+4] = argv[i];
    }
    args[i+4] = NULL;
    
    ret = execvp("bash", args);
    if (ret == -1) {
        error(1, errno, "failed to exec makepkg");
    }
}

/* The program is passed an extra parameter: the temporary file name to write package
 * paths to. This precedes any makepkg parameters, if any, which are passed on to
 * makepkg.
 */
int
main (int argc, char **argv)
{
    if (argc < 2) {
        error(1, 0, "supply a temporary file name to write package paths to");
    }
    setupBuildEnv();
    startMakepkg(argv[1], argc-2, argv+2);
    
    /* startMakepkg should never return. */
    error(1, 0, "internal error: reached the end of program");
}
