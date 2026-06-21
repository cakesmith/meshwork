# Tech Context

## Stack
- Kotlin/JVM
- Gradle wrapper pinned to 9.5.1
- Jackson YAML + JSON for parsing and data exchange
- JDK built-in `com.sun.net.httpserver.HttpServer` for HTTP place protocols
- Kotlin JSR223 scripting for `.kts` execution inside a dedicated worker JVM
- JUnit 5 and Kotlin test for verification
- Metrics implementation should stay dependency-free and use JDK timing/file/http primitives unless a future collector makes that impossible.

## Build Strategy
- `build.ps1` is a thin wrapper around the checked-in `gradlew.bat`.
- The helper sets `GRADLE_USER_HOME` to the repo-local cache and forwards requested tasks.
- Gradle caches stay repo-local through `.gradle-user-home/`.
- The current machine verification path runs successfully on Java 25.
- Windows `.exe` packaging uses the JDK's built-in `jpackage` via `.\build.ps1 packageWindowsExe`.
- The packaged app image lands in `build\jpackage\meshwork\`, with the launcher at `build\jpackage\meshwork\meshwork.exe`.

## Runtime Assumptions
- The current machine has Java available.
- External transition logic files are repo-relative.
- The runtime now supports built-in `kotlin-process` plus generic wrapper-driven `process` executors.
- Wrapper commands are argv arrays, not shell strings; supported placeholders are `{logicPath}`, `{projectRoot}`, `{java}`, and `{classpath}`.
- Persistent worker reuse is limited to the current `run` invocation or `run --server` session.
- Metrics collection should be cheap enough to leave on in development by timing around transition execution with `System.nanoTime()`.
- YAML and JSON enum parsing is case-insensitive, so protocol kinds such as `http`, `stdin`, and `stdout` can stay lowercase in config.
- Server mode binds HTTP to `127.0.0.1:8080` by default and can move to another host/port through CLI flags.
- The Windows app image bundles a private Java runtime, so end users do not need a separate system JRE to run `meshwork.exe`.
