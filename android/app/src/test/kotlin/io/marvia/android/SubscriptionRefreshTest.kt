package io.marvia.android

import android.app.Application
import android.content.Context
import android.os.PowerManager
import androidx.work.Configuration
import androidx.work.ListenableWorker
import androidx.work.NetworkType
import androidx.work.WorkInfo
import androidx.work.WorkManager
import androidx.work.testing.SynchronousExecutor
import androidx.work.testing.WorkManagerTestInitHelper
import androidx.work.testing.TestListenableWorkerBuilder
import kotlinx.coroutines.runBlocking
import org.junit.Assert.assertEquals
import org.junit.Assert.assertFalse
import org.junit.Assert.assertTrue
import org.junit.Before
import org.junit.Test
import org.junit.runner.RunWith
import org.robolectric.RobolectricTestRunner
import org.robolectric.RuntimeEnvironment
import org.robolectric.Shadows.shadowOf
import org.robolectric.annotation.Config
import java.util.concurrent.TimeUnit

@RunWith(RobolectricTestRunner::class)
@Config(application = Application::class, sdk = [28])
class SubscriptionRefreshTest {
    private lateinit var context: Context
    private lateinit var manager: WorkManager

    @Before
    fun prepareScheduler() {
        context = RuntimeEnvironment.getApplication()
        context.getSharedPreferences("veil", Context.MODE_PRIVATE).edit().clear().commit()
        WorkManagerTestInitHelper.initializeTestWorkManager(context,
            Configuration.Builder().setExecutor(SynchronousExecutor()).build())
        manager = WorkManager.getInstance(context)
    }

    @Test
    fun refreshIsScheduledWithinSixToTwelveHoursAndWaitsForNetworkAndBattery() {
        val spec = SubscriptionRefresh.request().workSpec
        assertEquals(TimeUnit.HOURS.toMillis(12), spec.intervalDuration)
        assertEquals(TimeUnit.HOURS.toMillis(6), spec.flexDuration)
        assertEquals(TimeUnit.HOURS.toMillis(6), spec.initialDelay)
        assertEquals(NetworkType.CONNECTED, spec.constraints.requiredNetworkType)
        assertTrue(spec.constraints.requiresBatteryNotLow())
        assertFalse(spec.expedited)
        assertTrue(spec.input.keyValueMap.isEmpty())
    }

    @Test
    fun reopeningTheAppDoesNotDuplicateOrPostponePeriodicRefresh() {
        SubscriptionRefresh.sync(context, true)
        val first = manager.getWorkInfosForUniqueWork(SubscriptionRefresh.WORK_NAME).get().single()
        SubscriptionRefresh.sync(context, true)
        val next = manager.getWorkInfosForUniqueWork(SubscriptionRefresh.WORK_NAME).get().single()
        assertEquals(first.id, next.id)
        assertEquals(first.nextScheduleTimeMillis, next.nextScheduleTimeMillis)
    }

    @Test
    fun addingTheFirstKeySchedulesRefreshAndRemovingTheLastCancelsIt() {
        val store = Store(context)
        store.subscriptions = listOf(Store.Subscription("Первый продавец", "https://one.example/sub"))
        assertEquals(WorkInfo.State.ENQUEUED, manager.getWorkInfosForUniqueWork(SubscriptionRefresh.WORK_NAME).get().single().state)
        store.subscriptions = emptyList()
        assertEquals(WorkInfo.State.CANCELLED, manager.getWorkInfosForUniqueWork(SubscriptionRefresh.WORK_NAME).get().single().state)
    }

    @Test
    fun batterySaverDefersRefreshWithoutContactingThePanel() = runBlocking {
        Store(context).subscriptions = listOf(Store.Subscription("Продавец", "https://panel.invalid/sub"))
        shadowOf(context.getSystemService(PowerManager::class.java)).setIsPowerSaveMode(true)
        val worker = TestListenableWorkerBuilder<SubscriptionRefreshWorker>(context).build()
        assertEquals(ListenableWorker.Result.retry(), worker.doWork())
    }

    @Test
    fun aPendingRefreshWithNoKeysFinishesWithoutContactingThePanel() = runBlocking {
        val worker = TestListenableWorkerBuilder<SubscriptionRefreshWorker>(context).build()
        assertEquals(ListenableWorker.Result.success(), worker.doWork())
    }
}
