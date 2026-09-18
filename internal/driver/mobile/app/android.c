// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

//go:build android

#include <android/log.h>
#include <dlfcn.h>
#include <errno.h>
#include <fcntl.h>
#include <stdint.h>
#include <stdbool.h>
#include <string.h>
#include "_cgo_export.h"

#define LOG_INFO(...) __android_log_print(ANDROID_LOG_INFO, "Fyne", __VA_ARGS__)
#define LOG_FATAL(...) __android_log_print(ANDROID_LOG_FATAL, "Fyne", __VA_ARGS__)

static jclass current_class;

// FindClass resolves a class through the class loader of the calling Java
// frame. ANativeActivity_onCreate is invoked from android.app.NativeActivity
// (a boot-class-loader framework class), so a plain FindClass cannot see
// APK-bundled classes like org/golang/app/PlaybackService. Load app classes
// explicitly through the activity's own class loader instead.
static jclass find_app_class(JNIEnv *env, jobject activity_obj, const char *class_name) {
	jclass activity_class = (*env)->GetObjectClass(env, activity_obj);
	jmethodID get_loader = (*env)->GetMethodID(env, activity_class, "getClassLoader", "()Ljava/lang/ClassLoader;");
	jobject loader = (*env)->CallObjectMethod(env, activity_obj, get_loader);
	jclass loader_class = (*env)->FindClass(env, "java/lang/ClassLoader");
	jmethodID load_class = (*env)->GetMethodID(env, loader_class, "loadClass", "(Ljava/lang/String;)Ljava/lang/Class;");
	jstring name = (*env)->NewStringUTF(env, class_name);
	jclass clazz = (jclass)(*env)->CallObjectMethod(env, loader, load_class, name);
	(*env)->DeleteLocalRef(env, name);
	if (clazz == NULL || (*env)->ExceptionCheck(env)) {
		(*env)->ExceptionClear(env);
		LOG_FATAL("cannot find %s", class_name);
		return NULL;
	}
	return clazz;
}

static jmethodID find_method(JNIEnv *env, jclass clazz, const char *name, const char *sig) {
	jmethodID m = (*env)->GetMethodID(env, clazz, name, sig);
	if (m == 0) {
		(*env)->ExceptionClear(env);
		LOG_FATAL("cannot find method %s %s", name, sig);
		return 0;
	}
	return m;
}

static jmethodID find_static_method(JNIEnv *env, jclass clazz, const char *name, const char *sig) {
	jmethodID m = (*env)->GetStaticMethodID(env, clazz, name, sig);
	if (m == 0) {
		(*env)->ExceptionClear(env);
		LOG_FATAL("cannot find method %s %s", name, sig);
		return 0;
	}
	return m;
}

static jmethodID key_rune_method;
static jmethodID show_keyboard_method;
static jmethodID hide_keyboard_method;
static jmethodID show_file_open_method;
static jmethodID show_file_save_method;
static jmethodID finish_method;
static jmethodID set_system_bars_visible_method;

static jclass playback_service_class;
static jmethodID media_session_update_method;
static jmethodID media_session_stop_method;

jint JNI_OnLoad(JavaVM* vm, void* reserved) {
	JNIEnv* env;
	if ((*vm)->GetEnv(vm, (void**)&env, JNI_VERSION_1_6) != JNI_OK) {
		return -1;
	}

	return JNI_VERSION_1_6;
}

static int main_running = 0;

// ensure we refresh context on resume in case something has changed...
void onResume(ANativeActivity *activity) {
	JNIEnv* env = activity->env;
	setCurrentContext(activity->vm, (*env)->NewGlobalRef(env, activity->clazz));
}

void onStart(ANativeActivity *activity) {}
void onPause(ANativeActivity *activity) {}
void onStop(ANativeActivity *activity) {}

// Entry point from our subclassed NativeActivity.
//
// By here, the Go runtime has been initialized (as we are running in
// -buildmode=c-shared) but the first time it is called, Go's main.main
// hasn't been called yet.
//
// The Activity may be created and destroyed multiple times throughout
// the life of a single process. Each time, onCreate is called.
void ANativeActivity_onCreate(ANativeActivity *activity, void* savedState, size_t savedStateSize) {
	if (!main_running) {
		JNIEnv* env = activity->env;

		// Note that activity->clazz is mis-named.
		current_class = (*env)->GetObjectClass(env, activity->clazz);
		current_class = (*env)->NewGlobalRef(env, current_class);
		key_rune_method = find_static_method(env, current_class, "getRune", "(III)I");
		show_keyboard_method = find_static_method(env, current_class, "showKeyboard", "(I)V");
		hide_keyboard_method = find_static_method(env, current_class, "hideKeyboard", "()V");
		show_file_open_method = find_static_method(env, current_class, "showFileOpen", "(Ljava/lang/String;)V");
		show_file_save_method = find_static_method(env, current_class, "showFileSave", "(Ljava/lang/String;Ljava/lang/String;)V");
		finish_method = find_method(env, current_class, "finishActivity", "()V");
		set_system_bars_visible_method = find_static_method(env, current_class, "setSystemBarsVisible", "(Z)V");

		playback_service_class = find_app_class(env, activity->clazz, "org.golang.app.PlaybackService");
		playback_service_class = (*env)->NewGlobalRef(env, playback_service_class);
		media_session_update_method = find_static_method(env, playback_service_class, "mediaSessionUpdate", "(Landroid/content/Context;Ljava/lang/String;Ljava/lang/String;Ljava/lang/String;[BZJJ)V");
		media_session_stop_method = find_static_method(env, playback_service_class, "mediaSessionStop", "(Landroid/content/Context;)V");

		setCurrentContext(activity->vm, (*env)->NewGlobalRef(env, activity->clazz));

		jmethodID getfilesdir = find_method(env, current_class, "getFilesDir", "()Ljava/io/File;");
		jobject filesdirfile = (jobject)(*env)->CallObjectMethod(env, activity->clazz, getfilesdir, NULL);
		jclass file_class = (*env)->GetObjectClass(env, filesdirfile);
		jmethodID getabsolutepath = find_method(env, file_class, "getAbsolutePath", "()Ljava/lang/String;");
		jstring jpath = (jstring)(*env)->CallObjectMethod(env, filesdirfile, getabsolutepath, NULL);
		const char* filesdir = (*env)->GetStringUTFChars(env, jpath, NULL);

		// Set FILESDIR
		if (setenv("FILESDIR", filesdir, 1) != 0) {
			LOG_INFO("setenv(\"FILESDIR\", \"%s\", 1) failed: %d", activity->internalDataPath, errno);
		}

		// Set TMPDIR.
		jmethodID gettmpdir = find_method(env, current_class, "getTmpdir", "()Ljava/lang/String;");
		jpath = (jstring)(*env)->CallObjectMethod(env, activity->clazz, gettmpdir, NULL);
		const char* tmpdir = (*env)->GetStringUTFChars(env, jpath, NULL);
		if (setenv("TMPDIR", tmpdir, 1) != 0) {
			LOG_INFO("setenv(\"TMPDIR\", \"%s\", 1) failed: %d", tmpdir, errno);
		}
		(*env)->ReleaseStringUTFChars(env, jpath, tmpdir);

		// Call the Go main.main.
		uintptr_t mainPC = (uintptr_t)dlsym(RTLD_DEFAULT, "main.main");
		if (!mainPC) {
			LOG_FATAL("missing main.main");
		}
		callMain(mainPC);
		main_running = 1;
	}

	// These functions match the methods on Activity, described at
	// http://developer.android.com/reference/android/app/Activity.html
	//
	// Note that onNativeWindowResized is not called on resize. Avoid it.
	// https://code.google.com/p/android/issues/detail?id=180645
	activity->callbacks->onStart = onStart;
	activity->callbacks->onResume = onResume;
	activity->callbacks->onSaveInstanceState = onSaveInstanceState;
	activity->callbacks->onPause = onPause;
	activity->callbacks->onStop = onStop;
	activity->callbacks->onDestroy = onDestroy;
	activity->callbacks->onWindowFocusChanged = onWindowFocusChanged;
	activity->callbacks->onNativeWindowCreated = onNativeWindowCreated;
	activity->callbacks->onNativeWindowRedrawNeeded = onNativeWindowRedrawNeeded;
	activity->callbacks->onNativeWindowDestroyed = onNativeWindowDestroyed;
	activity->callbacks->onInputQueueCreated = onInputQueueCreated;
	activity->callbacks->onInputQueueDestroyed = onInputQueueDestroyed;
	activity->callbacks->onConfigurationChanged = onConfigurationChanged;
	activity->callbacks->onLowMemory = onLowMemory;

	onCreate(activity);
}

// TODO(crawshaw): Test configuration on more devices.
static const EGLint RGBA_8888[] = {
	EGL_RENDERABLE_TYPE, EGL_OPENGL_ES2_BIT,
	EGL_SURFACE_TYPE, EGL_WINDOW_BIT,
	EGL_BLUE_SIZE, 8,
	EGL_GREEN_SIZE, 8,
	EGL_RED_SIZE, 8,
	EGL_ALPHA_SIZE, 8,
	EGL_DEPTH_SIZE, 16,
	EGL_CONFIG_CAVEAT, EGL_NONE,
	EGL_NONE
};

EGLDisplay display = NULL;
EGLSurface surface = NULL;
EGLContext context = NULL;

static char* initEGLDisplay() {
	display = eglGetDisplay(EGL_DEFAULT_DISPLAY);
	if (!eglInitialize(display, 0, 0)) {
		return "EGL initialize failed";
	}
	return NULL;
}

char* createEGLSurface(ANativeWindow* window) {
	char* err;
	EGLint numConfigs, format;
	EGLConfig config;

	if (display == 0) {
		if ((err = initEGLDisplay()) != NULL) {
			return err;
		}
	}

	if (!eglChooseConfig(display, RGBA_8888, &config, 1, &numConfigs)) {
		return "EGL choose RGBA_8888 config failed";
	}
	if (numConfigs <= 0) {
		return "EGL no config found";
	}

	eglGetConfigAttrib(display, config, EGL_NATIVE_VISUAL_ID, &format);
	if (ANativeWindow_setBuffersGeometry(window, 0, 0, format) != 0) {
		return "EGL set buffers geometry failed";
	}

	surface = eglCreateWindowSurface(display, config, window, NULL);
	if (surface == EGL_NO_SURFACE) {
		return "EGL create surface failed";
	}

    if (context == NULL) {
        const EGLint contextAttribs[] = { EGL_CONTEXT_CLIENT_VERSION, 2, EGL_NONE };
        context = eglCreateContext(display, config, EGL_NO_CONTEXT, contextAttribs);
    }

	if (eglMakeCurrent(display, surface, surface, context) == EGL_FALSE) {
		return "eglMakeCurrent failed";
	}
	return NULL;
}

char* destroyEGLSurface() {
	if (!eglDestroySurface(display, surface)) {
		return "EGL destroy surface failed";
	}
	return NULL;
}

void finish(JNIEnv* env, jobject ctx) {
    (*env)->CallVoidMethod(
        env,
        ctx,
        finish_method);
}

int32_t getKeyRune(JNIEnv* env, AInputEvent* e) {
	return (int32_t)(*env)->CallStaticIntMethod(
		env,
		current_class,
		key_rune_method,
		AInputEvent_getDeviceId(e),
		AKeyEvent_getKeyCode(e),
		AKeyEvent_getMetaState(e)
	);
}

void showKeyboard(JNIEnv* env, int keyboardType) {
	(*env)->CallStaticVoidMethod(
		env,
		current_class,
		show_keyboard_method,
		keyboardType
	);
}

void hideKeyboard(JNIEnv* env) {
	(*env)->CallStaticVoidMethod(
		env,
		current_class,
		hide_keyboard_method
	);
}

void showFileOpen(JNIEnv* env, char* mimes) {
    jstring mimesJString = (*env)->NewStringUTF(env, mimes);
    (*env)->CallStaticVoidMethod(
		env,
		current_class,
		show_file_open_method,
		mimesJString
	);
}

void showFileSave(JNIEnv* env, char* mimes, char* filename) {
    jstring mimesJString = (*env)->NewStringUTF(env, mimes);
    jstring filenameJString = (*env)->NewStringUTF(env, filename);
    (*env)->CallStaticVoidMethod(
		env,
		current_class,
		show_file_save_method,
		mimesJString,
		filenameJString
	);
}

void Java_org_golang_app_GoNativeActivity_filePickerReturned(JNIEnv *env, jclass clazz, jstring str) {
    const char* cstr = (*env)->GetStringUTFChars(env, str, JNI_FALSE);
	filePickerReturned((char*)cstr);
}

void Java_org_golang_app_GoNativeActivity_insetsChanged(JNIEnv *env, jclass clazz, int top, int bottom, int left, int right) {
    insetsChanged(top, bottom, left, right);
}

void Java_org_golang_app_GoNativeActivity_keyboardTyped(JNIEnv *env, jclass clazz, jstring str) {
    const char* cstr = (*env)->GetStringUTFChars(env, str, JNI_FALSE);
	keyboardTyped((char*)cstr);
}

void Java_org_golang_app_GoNativeActivity_keyboardDelete(JNIEnv *env, jclass clazz) {
    keyboardDelete();
}

void Java_org_golang_app_GoNativeActivity_backPressed(JNIEnv *env, jclass clazz) {
    onBackPressed();
}

void Java_org_golang_app_GoNativeActivity_setDarkMode(JNIEnv *env, jclass clazz, jboolean dark) {
    setDarkMode((bool)dark);
}

void setSystemBarsVisible(JNIEnv* env, bool visible) {
	if (set_system_bars_visible_method == 0) {
		return;
	}
	(*env)->CallStaticVoidMethod(
		env,
		current_class,
		set_system_bars_visible_method,
		(jboolean)visible
	);
}

// -------------------------------------------------------------------------
// PlaybackService (media session / foreground service) bridge.
// -------------------------------------------------------------------------

void Java_org_golang_app_PlaybackService_nativeMediaAction(JNIEnv *env, jclass clazz, jint action, jlong arg) {
	goMediaAction(action, arg);
}

void mediaSessionUpdate(JNIEnv* env, jobject ctx, char* title, char* artist, char* album, void* art, int artLen, bool playing, jlong posMs, jlong durMs) {
	if (media_session_update_method == 0) {
		return;
	}
	jstring jtitle = (*env)->NewStringUTF(env, title);
	jstring jartist = (*env)->NewStringUTF(env, artist);
	jstring jalbum = (*env)->NewStringUTF(env, album);
	jbyteArray jart = NULL;
	if (artLen >= 0) {
		jart = (*env)->NewByteArray(env, artLen);
		if (artLen > 0) {
			(*env)->SetByteArrayRegion(env, jart, 0, artLen, (jbyte*)art);
		}
	}
	(*env)->CallStaticVoidMethod(
		env,
		playback_service_class,
		media_session_update_method,
		ctx, jtitle, jartist, jalbum, jart, (jboolean)playing, posMs, durMs
	);
	(*env)->DeleteLocalRef(env, jtitle);
	(*env)->DeleteLocalRef(env, jartist);
	(*env)->DeleteLocalRef(env, jalbum);
	if (jart != NULL) {
		(*env)->DeleteLocalRef(env, jart);
	}
}

void mediaSessionStop(JNIEnv* env, jobject ctx) {
	if (media_session_stop_method == 0) {
		return;
	}
	(*env)->CallStaticVoidMethod(
		env,
		playback_service_class,
		media_session_stop_method,
		ctx
	);
}
