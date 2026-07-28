// Обработчики навешиваются здесь, а не атрибутами onclick/onchange в разметке:
// строгий CSP (script-src 'self') инлайновый JS не выполняет.
document.addEventListener('DOMContentLoaded', () => {
    const header = document.querySelector('.filters-header');
    if (header) {
        header.addEventListener('click', () => toggleFilters(header));
    }

    document.querySelectorAll('.filter-select').forEach(select => {
        select.addEventListener('change', () => updateFilterStyle(select));
    });

    const clearBtn = document.getElementById('clear-filters');
    if (clearBtn) {
        clearBtn.addEventListener('click', clearFilters);
    }
});

function toggleFilters(header) {
    header.classList.toggle('active');
    document.getElementById('filters-body').classList.toggle('show');
}

function updateFilterStyle(select) {
    if (select.value) {
        select.classList.add('has-value');
    } else {
        select.classList.remove('has-value');
    }
    updateActiveFiltersTags();
}

function updateActiveFiltersTags() {
    const container = document.getElementById('active-filters');
    const selects = document.querySelectorAll('.filter-select');

    // Подписи фильтров — это имена авторов, статусов и очередей из трекера,
    // поэтому узлы собираются через textContent, а не склейкой разметки.
    container.replaceChildren();
    selects.forEach(select => {
        if (!select.value) return;

        const tag = document.createElement('span');
        tag.className = 'active-filter-tag';
        tag.appendChild(document.createTextNode(select.options[select.selectedIndex].text + ' '));

        const remove = document.createElement('span');
        remove.className = 'remove';
        remove.textContent = '✕';
        remove.addEventListener('click', () => clearFilter(select.name));

        tag.appendChild(remove);
        container.appendChild(tag);
    });
}

function clearFilter(name) {
    const select = document.querySelector(`select[name="${name}"]`);
    if (select) {
        select.value = '';
        select.classList.remove('has-value');
        updateActiveFiltersTags();
        htmx.trigger('#search-form', 'submit');
    }
}

function clearFilters() {
    document.querySelectorAll('.filter-select').forEach(select => {
        select.value = '';
        select.classList.remove('has-value');
    });
    updateActiveFiltersTags();
    htmx.trigger('#search-form', 'submit');
}
