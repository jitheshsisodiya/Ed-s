import java.util.Properties

plugins {
    id("com.android.application")
    id("kotlin-android")
    // The Flutter Gradle Plugin must be applied after the Android and Kotlin
    // Gradle plugins.
    id("dev.flutter.flutter-gradle-plugin")
}

// Release signing is read from android/key.properties, which is not in the
// repository — the keystore and its password are what let somebody publish an
// update Android accepts as this app, so they belong to whoever ships it, not
// to the source. Without that file the build falls back to the debug key,
// which installs fine on your own device and is refused by Play.
val keystoreProperties = Properties().apply {
    val f = rootProject.file("key.properties")
    if (f.exists()) f.inputStream().use { load(it) }
}

android {
    namespace = "com.nexusvpn.mobile"
    compileSdk = flutter.compileSdkVersion
    ndkVersion = flutter.ndkVersion

    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }

    kotlinOptions {
        jvmTarget = JavaVersion.VERSION_17.toString()
    }

    defaultConfig {
        // Also update the bundle id in ios/Runner/Info.plist and the
        // Network Extension bundle id constant in
        // lib/core/vpn_controller.dart if this ever changes.
        applicationId = "com.nexusvpn.mobile"
        // wireguard_flutter requires SDK 21+ for VpnService; NexusVPN
        // targets Flutter's tooling-managed default (currently higher)
        // for broader plugin/API compatibility.
        minSdk = maxOf(flutter.minSdkVersion, 24)
        targetSdk = flutter.targetSdkVersion
        versionCode = flutter.versionCode
        versionName = flutter.versionName
        multiDexEnabled = true
    }

    signingConfigs {
        if (keystoreProperties.getProperty("storeFile") != null) {
            create("release") {
                storeFile = file(keystoreProperties.getProperty("storeFile"))
                storePassword = keystoreProperties.getProperty("storePassword")
                keyAlias = keystoreProperties.getProperty("keyAlias")
                keyPassword = keystoreProperties.getProperty("keyPassword")
            }
        }
    }

    buildTypes {
        release {
            signingConfig = signingConfigs.findByName("release")
                ?: signingConfigs.getByName("debug")
            // Kept off deliberately. Shrinking strips the reflection-reached
            // classes inside the WireGuard plugin's VpnService, and the
            // failure is a tunnel that will not start on a release build
            // only — the build everybody actually installs.
            isMinifyEnabled = false
            isShrinkResources = false
        }
    }
}

flutter {
    source = "../.."
}

dependencies {
    implementation("androidx.multidex:multidex:2.0.1")
}
