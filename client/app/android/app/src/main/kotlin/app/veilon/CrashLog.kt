package app.veilon

import android.content.Context
import android.os.Handler
import android.os.Looper
import android.os.SystemClock
import java.io.File
import java.io.PrintWriter
import java.io.StringWriter
import java.text.SimpleDateFormat
import java.util.Date
import java.util.Locale
import veiloncore.Veiloncore

/**
 * Crash containment + the persisted crash report.
 *
 * 1.0.6 ("the app must never close"):
 * - [report] records an exception that our own guards caught (service
 *   callbacks, core tasks, channel handlers) to filesDir/core/last-crash.txt
 *   and the log page; the app keeps running.
 * - Uncaught exceptions on any background thread are recorded and the
 *   thread just ends - the process is NOT killed.
 * - Uncaught exceptions on the main thread are recorded and the main looper
 *   is re-entered ([guardMainLooper]), so the UI keeps running. Safety valve:
 *   more than [MAX_MAIN_CRASHES] within [WINDOW_MS] (a loop that cannot
 *   recover) falls back to the platform handler, as before 1.0.6.
 * - Veiloncore.initCrashLog routes fatal Go runtime reports to
 *   filesDir/core/go-crash.txt and, on the next start, moves the previous
 *   one into last-crash.txt and onto the log page. (A fatal Go runtime error
 *   in a library goroutine cannot be caught by anything in the process.)
 */
object CrashLog {
    @Volatile private var installed = false
    @Volatile private var dir: File? = null
    private const val MAX_MAIN_CRASHES = 5
    private const val WINDOW_MS = 10_000L
    private val mainCrashTimes = ArrayDeque<Long>()

    fun install(ctx: Context) {
        val app = ctx.applicationContext
        val d = runCatching { Store.dataDir(app) }.getOrNull()
        if (d != null) dir = d
        if (!installed) {
            synchronized(this) {
                if (!installed) {
                    installed = true
                    val previous = Thread.getDefaultUncaughtExceptionHandler()
                    Thread.setDefaultUncaughtExceptionHandler { thread, e ->
                        runCatching { record(thread, e, "uncaught") }
                        if (thread === Looper.getMainLooper().thread) {
                            // Only reached when the looper guard gave up (or
                            // was not installed yet): let the platform decide.
                            if (previous != null) previous.uncaughtException(thread, e)
                            else { android.os.Process.killProcess(android.os.Process.myPid()); System.exit(10) }
                        }
                        // Background thread: recorded, the thread ends, the app lives.
                    }
                    guardMainLooper(previous)
                }
            }
        }
        // Go side (needs the core library loaded; harmless if it fails).
        if (d != null) runCatching { Veiloncore.initCrashLog(d.absolutePath) }
    }

    /**
     * Re-enters Looper.loop() after an exception escaped a main-thread
     * message, so one bad callback does not close the app.
     */
    private fun guardMainLooper(previous: Thread.UncaughtExceptionHandler?) {
        Handler(Looper.getMainLooper()).post {
            while (true) {
                try {
                    Looper.loop()
                    return@post // the looper quit (process ending)
                } catch (e: Throwable) {
                    runCatching { record(Thread.currentThread(), e, "main thread, contained") }
                    val now = SystemClock.elapsedRealtime()
                    val giveUp = synchronized(mainCrashTimes) {
                        mainCrashTimes.addLast(now)
                        while (mainCrashTimes.isNotEmpty() && now - mainCrashTimes.first() > WINDOW_MS) mainCrashTimes.removeFirst()
                        mainCrashTimes.size > MAX_MAIN_CRASHES
                    }
                    if (giveUp) {
                        val t = Thread.currentThread()
                        if (previous != null) previous.uncaughtException(t, e)
                        else { android.os.Process.killProcess(android.os.Process.myPid()); System.exit(10) }
                        return@post
                    }
                }
            }
        }
    }

    /** An exception one of our guards caught: record it, keep running. */
    fun report(thread: Thread, e: Throwable, where: String = "caught") {
        runCatching { record(thread, e, where) }
    }

    private fun record(thread: Thread, e: Throwable, where: String) {
        val sw = StringWriter()
        e.printStackTrace(PrintWriter(sw))
        runCatching { Veiloncore.writeLog("error", "app: $where: ${e.javaClass.simpleName}: ${e.message ?: ""}") }
        val d = dir ?: return
        val ts = SimpleDateFormat("yyyy-MM-dd'T'HH:mm:ssZ", Locale.US).format(Date())
        val entry = "==== $ts  kotlin ($where, thread ${thread.name})\n$sw\n"
        synchronized(this) {
            val f = File(d, "last-crash.txt")
            val old = if (f.exists()) f.readText() else ""
            var all = old + entry
            if (all.length > MAX) all = all.takeLast(MAX)
            f.writeText(all)
        }
    }

    private const val MAX = 64 * 1024
}
