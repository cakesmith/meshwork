import org.jetbrains.kotlin.gradle.dsl.JvmTarget

plugins {
    kotlin("jvm") version "2.1.21"
    application
}

group = "dev.meshwork"
version = "0.1.0"

repositories {
    mavenCentral()
}

dependencies {
    implementation("com.fasterxml.jackson.core:jackson-databind:2.18.2")
    implementation("com.fasterxml.jackson.module:jackson-module-kotlin:2.18.2")
    implementation("com.fasterxml.jackson.dataformat:jackson-dataformat-yaml:2.18.2")
    implementation("org.jetbrains.kotlin:kotlin-scripting-jsr223:2.1.21")

    testImplementation(kotlin("test"))
    testImplementation("org.junit.jupiter:junit-jupiter:5.11.4")
}

kotlin {
    compilerOptions {
        jvmTarget.set(JvmTarget.JVM_21)
    }
}

application {
    mainClass.set("dev.meshwork.cli.MeshworkCliKt")
}

java {
    sourceCompatibility = JavaVersion.VERSION_21
    targetCompatibility = JavaVersion.VERSION_21
}

tasks.test {
    useJUnitPlatform()
}

val windowsAppName = project.name
val windowsMainJar = "$windowsAppName-$version.jar"
val windowsMainClass = application.mainClass.get()
val windowsJpackage = file("${System.getProperty("java.home")}\\bin\\jpackage.exe").absolutePath
val windowsJpackageOutputDir = layout.buildDirectory.dir("jpackage")
val windowsJpackageInputDir = layout.buildDirectory.dir("install/$windowsAppName/lib")

tasks.register<Exec>("packageWindowsExe") {
    group = "distribution"
    description = "Builds a Windows app image with a meshwork.exe launcher."
    dependsOn(tasks.installDist)
    notCompatibleWithConfigurationCache("Uses jpackage directly to build a Windows app image.")
    onlyIf { System.getProperty("os.name").startsWith("Windows") }
    onlyIf { file(windowsJpackage).exists() }
    inputs.dir(windowsJpackageInputDir)
    outputs.dir(windowsJpackageOutputDir.map { it.dir(windowsAppName) })
    doFirst {
        delete(windowsJpackageOutputDir.get().file(windowsAppName))
    }
    commandLine(
        windowsJpackage,
        "--type", "app-image",
        "--dest", windowsJpackageOutputDir.get().asFile.absolutePath,
        "--name", windowsAppName,
        "--input", windowsJpackageInputDir.get().asFile.absolutePath,
        "--main-jar", windowsMainJar,
        "--main-class", windowsMainClass,
        "--win-console",
    )
}
