package org.golang.app;

import android.app.Notification;
import android.app.NotificationChannel;
import android.app.NotificationManager;
import android.app.PendingIntent;
import android.app.Service;
import android.content.BroadcastReceiver;
import android.content.ComponentName;
import android.content.Context;
import android.content.Intent;
import android.content.IntentFilter;
import android.content.pm.PackageManager;
import android.graphics.Bitmap;
import android.graphics.BitmapFactory;
import android.media.AudioManager;
import android.media.AudioAttributes;
import android.media.AudioFocusRequest;
import android.media.MediaMetadata;
import android.media.session.MediaSession;
import android.media.session.PlaybackState;
import android.os.Build;
import android.os.IBinder;
import android.util.Log;

// PlaybackService keeps audio playing while the app is backgrounded and
// surfaces it to the system: a foreground notification with transport
// controls, a MediaSession (lock screen, Bluetooth AVRCP, headset buttons),
// audio focus (calls, other apps) and the becoming-noisy broadcast (headphone
// unplug). Everything is framework API — the fyne dex toolchain has no
// androidx available.
//
// The service lives in the app's single process alongside the Go runtime;
// events are forwarded to Go via nativeMediaAction, and Go drives the service
// through the static mediaSessionUpdate / mediaSessionStop helpers (called
// over JNI). It is deliberately not sticky: if the process died, the Go
// player died with it, and restarting the service alone is pointless.
public class PlaybackService extends Service {
	// Action codes forwarded to Go. Keep in sync with the Go side
	// (fyne.io/fyne/v2/driver/mobile MediaAction constants).
	static final int ACT_PLAY = 1;
	static final int ACT_PAUSE = 2;
	static final int ACT_NEXT = 3;
	static final int ACT_PREVIOUS = 4;
	static final int ACT_STOP = 5;
	static final int ACT_SEEK = 6;         // arg: position ms
	static final int ACT_FOCUS_LOSS = 10;
	static final int ACT_FOCUS_LOSS_TRANSIENT = 11;
	static final int ACT_FOCUS_DUCK = 12;
	static final int ACT_FOCUS_GAIN = 13;
	static final int ACT_BECOMING_NOISY = 14;

	private static final String CHANNEL_ID = "media_playback";
	private static final int NOTIFICATION_ID = 1;

	private static native void nativeMediaAction(int action, long arg);

	private MediaSession session;
	private AudioManager audioManager;
	private AudioFocusRequest focusRequest; // API 26+
	private BroadcastReceiver noisyReceiver;
	private Bitmap artwork;
	private String title = "";
	private String artist = "";
	private String album = "";
	private long durationMs = 0;
	private boolean playing = false;
	private long positionMs = 0;
	private boolean focusHeld = false;
	private static boolean notifPermRequested = false;

	// ---------------------------------------------------------------------
	// Static entry points called from Go via JNI (any thread).
	// ---------------------------------------------------------------------

	// mediaSessionUpdate pushes the latest metadata and playback state. It
	// starts the service (foreground, with the notification) when needed.
	// art == null keeps the previous artwork; art.length == 0 clears it.
	public static void mediaSessionUpdate(Context ctx, String title, String artist, String album,
			byte[] art, boolean playing, long posMs, long durMs) {
		maybeRequestNotificationPermission(ctx);
		Intent i = new Intent(ctx, PlaybackService.class);
		i.setAction("org.golang.app.MEDIA_UPDATE");
		i.putExtra("title", title != null ? title : "");
		i.putExtra("artist", artist != null ? artist : "");
		i.putExtra("album", album != null ? album : "");
		if (art != null) {
			i.putExtra("art", art);
		}
		i.putExtra("hasArt", art != null);
		i.putExtra("playing", playing);
		i.putExtra("pos", posMs);
		i.putExtra("dur", durMs);
		startServiceSafely(ctx, i);
	}

	public static void mediaSessionStop(Context ctx) {
		Intent i = new Intent(ctx, PlaybackService.class);
		i.setAction("org.golang.app.MEDIA_TEARDOWN");
		startServiceSafely(ctx, i);
	}

	private static void startServiceSafely(Context ctx, Intent i) {
		try {
			if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
				ctx.startForegroundService(i);
			} else {
				ctx.startService(i);
			}
		} catch (Exception e) {
			Log.e("Fyne", "could not start PlaybackService", e);
		}
	}

	// The notification is mandatory for a foreground service; on API 33+ its
	// visibility needs the runtime permission. Ask once, at first playback.
	private static void maybeRequestNotificationPermission(Context ctx) {
		if (notifPermRequested || Build.VERSION.SDK_INT < 33) {
			return;
		}
		notifPermRequested = true;
		try {
			if (ctx.checkSelfPermission("android.permission.POST_NOTIFICATIONS")
					!= PackageManager.PERMISSION_GRANTED && ctx instanceof android.app.Activity) {
				((android.app.Activity) ctx).requestPermissions(
						new String[]{"android.permission.POST_NOTIFICATIONS"}, 42);
			}
		} catch (Exception e) {
			Log.e("Fyne", "notification permission request failed", e);
		}
	}

	// ---------------------------------------------------------------------
	// Service lifecycle
	// ---------------------------------------------------------------------

	@Override
	public void onCreate() {
		super.onCreate();
		audioManager = (AudioManager) getSystemService(Context.AUDIO_SERVICE);

		session = new MediaSession(this, "FynePlayback");
		session.setFlags(MediaSession.FLAG_HANDLES_MEDIA_BUTTONS
				| MediaSession.FLAG_HANDLES_TRANSPORT_CONTROLS);
		session.setCallback(new MediaSession.Callback() {
			@Override public void onPlay() { nativeMediaAction(ACT_PLAY, 0); }
			@Override public void onPause() { nativeMediaAction(ACT_PAUSE, 0); }
			@Override public void onSkipToNext() { nativeMediaAction(ACT_NEXT, 0); }
			@Override public void onSkipToPrevious() { nativeMediaAction(ACT_PREVIOUS, 0); }
			@Override public void onStop() { nativeMediaAction(ACT_STOP, 0); }
			@Override public void onSeekTo(long pos) { nativeMediaAction(ACT_SEEK, pos); }
		});
		session.setPlaybackToLocal(new AudioAttributes.Builder()
				.setUsage(AudioAttributes.USAGE_MEDIA)
				.setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
				.build());
		session.setActive(true);

		noisyReceiver = new BroadcastReceiver() {
			@Override public void onReceive(Context context, Intent intent) {
				nativeMediaAction(ACT_BECOMING_NOISY, 0);
			}
		};
		registerReceiver(noisyReceiver, new IntentFilter(AudioManager.ACTION_AUDIO_BECOMING_NOISY));
	}

	@Override
	public IBinder onBind(Intent intent) {
		return null;
	}

	@Override
	public int onStartCommand(Intent intent, int flags, int startId) {
		if (intent == null || intent.getAction() == null) {
			return START_NOT_STICKY;
		}
		switch (intent.getAction()) {
		case "org.golang.app.MEDIA_UPDATE":
			title = intent.getStringExtra("title");
			artist = intent.getStringExtra("artist");
			album = intent.getStringExtra("album");
			if (intent.getBooleanExtra("hasArt", false)) {
				byte[] art = intent.getByteArrayExtra("art");
				if (art != null && art.length > 0) {
					artwork = BitmapFactory.decodeByteArray(art, 0, art.length);
				} else {
					artwork = null;
				}
			}
			playing = intent.getBooleanExtra("playing", false);
			positionMs = intent.getLongExtra("pos", 0);
			durationMs = intent.getLongExtra("dur", 0);
			if (playing) {
				requestFocus();
			}
			pushSession();
			startForeground(NOTIFICATION_ID, buildNotification());
			return START_NOT_STICKY;
		case "org.golang.app.MEDIA_TEARDOWN":
			teardown();
			return START_NOT_STICKY;
		}
		// Notification button actions are plain intents to the service.
		String a = intent.getAction();
		if (a.startsWith("org.golang.app.MEDIA_ACT_")) {
			int act = Integer.parseInt(a.substring("org.golang.app.MEDIA_ACT_".length()));
			nativeMediaAction(act, intent.getLongExtra("arg", 0));
		}
		return START_NOT_STICKY;
	}

	private void teardown() {
		abandonFocus();
		session.setActive(false);
		session.release();
		try {
			unregisterReceiver(noisyReceiver);
		} catch (Exception e) {
			// already unregistered
		}
		stopForeground(true);
		stopSelf();
	}

	@Override
	public void onDestroy() {
		abandonFocus();
		if (session != null) {
			session.setActive(false);
			session.release();
			session = null;
		}
		if (noisyReceiver != null) {
			try {
				unregisterReceiver(noisyReceiver);
			} catch (Exception e) {
				// already unregistered
			}
			noisyReceiver = null;
		}
		super.onDestroy();
	}

	// ---------------------------------------------------------------------
	// Audio focus: forward to Go; the policy (pause / duck / resume) lives
	// there where it is testable.
	// ---------------------------------------------------------------------

	private final AudioManager.OnAudioFocusChangeListener focusListener =
			new AudioManager.OnAudioFocusChangeListener() {
		@Override public void onAudioFocusChange(int change) {
			switch (change) {
			case AudioManager.AUDIOFOCUS_LOSS:
				nativeMediaAction(ACT_FOCUS_LOSS, 0);
				break;
			case AudioManager.AUDIOFOCUS_LOSS_TRANSIENT:
				nativeMediaAction(ACT_FOCUS_LOSS_TRANSIENT, 0);
				break;
			case AudioManager.AUDIOFOCUS_LOSS_TRANSIENT_CAN_DUCK:
				nativeMediaAction(ACT_FOCUS_DUCK, 0);
				break;
			case AudioManager.AUDIOFOCUS_GAIN:
				nativeMediaAction(ACT_FOCUS_GAIN, 0);
				break;
			}
		}
	};

	private void requestFocus() {
		if (focusHeld || audioManager == null) {
			return;
		}
		int res;
		if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
			focusRequest = new AudioFocusRequest.Builder(AudioManager.AUDIOFOCUS_GAIN)
					.setOnAudioFocusChangeListener(focusListener)
					.build();
			res = audioManager.requestAudioFocus(focusRequest);
		} else {
			res = audioManager.requestAudioFocus(focusListener,
					AudioManager.STREAM_MUSIC, AudioManager.AUDIOFOCUS_GAIN);
		}
		focusHeld = (res == AudioManager.AUDIOFOCUS_REQUEST_GRANTED);
	}

	private void abandonFocus() {
		if (!focusHeld || audioManager == null) {
			return;
		}
		if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O && focusRequest != null) {
			audioManager.abandonAudioFocusRequest(focusRequest);
		} else {
			audioManager.abandonAudioFocus(focusListener);
		}
		focusHeld = false;
	}

	// ---------------------------------------------------------------------
	// Session + notification rendering
	// ---------------------------------------------------------------------

	private void pushSession() {
		MediaMetadata.Builder mb = new MediaMetadata.Builder()
				.putString(MediaMetadata.METADATA_KEY_TITLE, title)
				.putString(MediaMetadata.METADATA_KEY_ARTIST, artist)
				.putString(MediaMetadata.METADATA_KEY_ALBUM, album)
				.putLong(MediaMetadata.METADATA_KEY_DURATION, durationMs);
		if (artwork != null) {
			mb.putBitmap(MediaMetadata.METADATA_KEY_ALBUM_ART, artwork);
		}
		session.setMetadata(mb.build());

		int state = playing ? PlaybackState.STATE_PLAYING : PlaybackState.STATE_PAUSED;
		session.setPlaybackState(new PlaybackState.Builder()
				.setActions(PlaybackState.ACTION_PLAY | PlaybackState.ACTION_PAUSE
						| PlaybackState.ACTION_PLAY_PAUSE | PlaybackState.ACTION_SKIP_TO_NEXT
						| PlaybackState.ACTION_SKIP_TO_PREVIOUS | PlaybackState.ACTION_SEEK_TO
						| PlaybackState.ACTION_STOP)
				.setState(state, positionMs, 1.0f)
				.build());
	}

	private PendingIntent actionIntent(int act, int requestCode) {
		Intent i = new Intent(this, PlaybackService.class);
		i.setAction("org.golang.app.MEDIA_ACT_" + act);
		int flags = PendingIntent.FLAG_UPDATE_CURRENT;
		if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
			flags |= PendingIntent.FLAG_IMMUTABLE;
		}
		return PendingIntent.getService(this, requestCode, i, flags);
	}

	private Notification buildNotification() {
		NotificationManager nm = (NotificationManager) getSystemService(Context.NOTIFICATION_SERVICE);
		Notification.Builder b;
		if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.O) {
			if (nm != null && nm.getNotificationChannel(CHANNEL_ID) == null) {
				nm.createNotificationChannel(new NotificationChannel(CHANNEL_ID,
						"Playback", NotificationManager.IMPORTANCE_LOW));
			}
			b = new Notification.Builder(this, CHANNEL_ID);
		} else {
			b = new Notification.Builder(this);
		}

		int icon = getApplicationInfo().icon;
		if (icon == 0) {
			icon = android.R.drawable.ic_media_play;
		}
		b.setSmallIcon(icon);
		b.setContentTitle(title);
		String sub = artist;
		if (sub == null || sub.isEmpty()) {
			sub = album;
		} else if (album != null && !album.isEmpty()) {
			sub = sub + " · " + album;
		}
		b.setContentText(sub);
		if (artwork != null) {
			b.setLargeIcon(artwork);
		}
		b.setVisibility(Notification.VISIBILITY_PUBLIC);
		b.setOngoing(playing);

		Intent launch = new Intent(this, GoNativeActivity.class);
		int piFlags = PendingIntent.FLAG_UPDATE_CURRENT;
		if (Build.VERSION.SDK_INT >= Build.VERSION_CODES.M) {
			piFlags |= PendingIntent.FLAG_IMMUTABLE;
		}
		b.setContentIntent(PendingIntent.getActivity(this, 0, launch, piFlags));

		b.addAction(new Notification.Action.Builder(
				android.R.drawable.ic_media_previous, "Previous", actionIntent(ACT_PREVIOUS, 1)).build());
		b.addAction(new Notification.Action.Builder(
				playing ? android.R.drawable.ic_media_pause : android.R.drawable.ic_media_play,
				playing ? "Pause" : "Play", actionIntent(playing ? ACT_PAUSE : ACT_PLAY, 2)).build());
		b.addAction(new Notification.Action.Builder(
				android.R.drawable.ic_media_next, "Next", actionIntent(ACT_NEXT, 3)).build());

		Notification.MediaStyle style = new Notification.MediaStyle()
				.setMediaSession(session.getSessionToken())
				.setShowActionsInCompactView(0, 1, 2);
		b.setStyle(style);
		return b.build();
	}
}
