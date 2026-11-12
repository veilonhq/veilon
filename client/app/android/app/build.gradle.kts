import java.util.Properties

plugins {
    id("com.android.application")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

android {
    namespace = "app.veilon"
    compileSdk = 36
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    defaultConfig {
        applicationId = "app.veilon"
        minSdk = 24
        targetSdk = 36
        // ABIs come from `flutter build apk --target-platform android-arm,android-arm64`
        // (ndk.abiFilters conflicts with --split-per-abi); the AAR ships only these two.
        // Uses the version code from pubspec.yaml. When using split APKs, 1000 * ABI_VERSION
        // is added automatically by Flutter. (https://developer.android.com/studio/build/configure-apk-splits#configure-APK-versions)
        // You can force using the value of versionCode by specifying the `-P force-version-code-ignoring-abi=true`
        // flag during build.
        versionCode = flutter.versionCode
        versionName = flutter.versionName
    }

    packaging {
        jniLibs {
            // Extract native libs (gomobile .so) on install.
            useLegacyPackaging = true
        }
    }

    // Release signing is not in the repo. Put your own key in
    // android/key.properties (storeFile, storePassword, keyAlias, keyPassword;
    // storeFile is relative to android/). Without it, release builds fall back
    // to <client>/keystore/veilon-release.jks + store-password.txt, then to the
    // debug key so `flutter run --release` still works.
    val keyProps = Properties()
    val keyPropsFile = rootProject.file("key.properties")
    if (keyPropsFile.exists()) keyPropsFile.inputStream().use { keyProps.load(it) }
    val ksDir = rootProject.file("../../keystore")
    val ksFile = File(ksDir, "veilon-release.jks")
    val ksPass = File(ksDir, "store-password.txt")
    signingConfigs {
        if (keyPropsFile.exists()) {
            create("release") {
                storeFile = rootProject.file(keyProps.getProperty("storeFile"))
                storePassword = keyProps.getProperty("storePassword")
                keyAlias = keyProps.getProperty("keyAlias")
                keyPassword = keyProps.getProperty("keyPassword")
                enableV1Signing = true
                enableV2Signing = true
                enableV3Signing = true
            }
        } else if (ksFile.exists() && ksPass.exists()) {
            create("release") {
                storeFile = ksFile
                storeType = "PKCS12"
                storePassword = ksPass.readText().trim()
                keyAlias = "veilon"
                keyPassword = ksPass.readText().trim()
                enableV1Signing = true
                enableV2Signing = true
                enableV3Signing = true
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release") ?: signingConfigs.getByName("debug")
        }
    }
}

kotlin {
    compilerOptions {
        jvmTarget = org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17
    }
}

flutter {
    source = "../.."
}

dependencies {
    // gomobile bind output (veiloncore.aar) dropped into app/libs/.
    implementation(fileTree(mapOf("dir" to "libs", "include" to listOf("*.aar"))))
    implementation("com.journeyapps:zxing-android-embedded:4.3.0")
    // FileProvider for the in-app update (already in the graph via the Flutter embedding).
    implementation("androidx.core:core:1.13.1")
}
