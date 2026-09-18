plugins {
    id("com.android.application")
    kotlin("plugin.serialization")
    kotlin("plugin.compose")
}

android {
    namespace = "dev.wicolian.boxdeck.wear"
    compileSdk = 37
    defaultConfig {
        applicationId = "dev.wicolian.boxdeck.wear"
        minSdk = 30
        targetSdk = 37
        versionCode = 1
        versionName = "0.1.0"
    }
    buildFeatures { compose = true }
    compileOptions {
        sourceCompatibility = JavaVersion.VERSION_17
        targetCompatibility = JavaVersion.VERSION_17
    }
    kotlin {
        compilerOptions {
            jvmTarget.set(org.jetbrains.kotlin.gradle.dsl.JvmTarget.JVM_17)
        }
    }
    packaging { resources.excludes += "/META-INF/{AL2.0,LGPL2.1}" }
}

dependencies {
    implementation(project(":core"))
    implementation("org.jetbrains.kotlinx:kotlinx-serialization-json:1.8.1")
    implementation("androidx.core:core-ktx:1.18.0")
    implementation("androidx.security:security-crypto:1.1.0-alpha07")
    implementation(platform("androidx.compose:compose-bom:2026.08.00"))
    implementation("androidx.activity:activity-compose:1.13.0")
    implementation("androidx.compose.ui:ui")
    implementation("androidx.compose.ui:ui-tooling-preview")
    implementation("androidx.wear.compose:compose-foundation:1.6.2")
    implementation("androidx.wear.compose:compose-material:1.6.2")
    implementation("androidx.wear.compose:compose-navigation:1.6.2")
    implementation("androidx.wear:wear:1.4.0")
    implementation("androidx.wear.tiles:tiles:1.6.2")
    implementation("androidx.concurrent:concurrent-futures:1.2.0")
    implementation("androidx.wear.protolayout:protolayout-material:1.4.2")
    implementation("com.google.android.horologist:horologist-tiles:0.7.15")
    implementation("com.google.android.gms:play-services-wearable:20.0.1")
    implementation("androidx.wear.watchface:watchface-complications-data-source-ktx:1.2.1")
    implementation("org.jetbrains.kotlinx:kotlinx-coroutines-android:1.10.2")
}
