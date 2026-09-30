package io.marvia.android

/** Политика выбора приложений отделена от Android, чтобы проверять удалённые пакеты. */
object AppRouting {
    /**
     * add возвращает false, если пакет уже удалён. Нельзя незаметно расширять
     * режим «только эти» до всего телефона, когда не удалось добавить ни одного.
     */
    fun addIncluded(packages: Set<String>, own: String, add: (String) -> Boolean): Boolean {
        var added = 0
        for (pkg in packages) {
            if (pkg != own && add(pkg)) added++
        }
        return packages.isEmpty() || added > 0
    }
}
