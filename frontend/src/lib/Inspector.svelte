<script>
  import { dotsToMm, mmToDots, FONTS, removeSelected, duplicateSelected, moveSelected, ui } from './state.svelte.js'
  import { showToast } from './toast.svelte.js'
  import { SYMBOLOGIES, SYM, renderBarcode, isValidFor } from './barcode.js'

  let { el } = $props()

  const titles = {
    text: 'Текст', image: 'Изображение', rect: 'Фигура',
    shape: null, // заголовок зависит от kind — см. title ниже
    barcode: 'Штрихкод',
  }
  const kindTitles = { ellipse: 'Эллипс', triangle: 'Треугольник' }
  const title = $derived(titles[el.type] ?? kindTitles[el.kind] ?? 'Фигура')

  const bc = $derived(el.type === 'barcode' ? renderBarcode(el) : null)
  const sym = $derived(el.type === 'barcode' ? SYM[el.symbology] : null)

  // при смене типа: модуль по умолчанию, пример значения, если текущее не подходит
  function changeSymbology(e) {
    const id = e.currentTarget.value
    if (!isValidFor(id, el.value)) el.value = SYM[id].sample
    el.module = SYM[id].module
    el.symbology = id
    ui.dirty = true
  }

  // поля в мм пишем в точках
  const setMm = (key) => (e) => {
    const v = parseFloat(e.currentTarget.value)
    if (!Number.isNaN(v)) {
      el[key] = Math.max(key === 'x' || key === 'y' ? -10000 : 1, mmToDots(v))
      ui.dirty = true
    }
  }
</script>

  <section class="panel">
    <h3>{title}</h3>

  {#if el.type === 'text'}
    <label class="field wide">
      <span>Содержимое</span>
      <textarea rows="3" bind:value={el.text}></textarea>
    </label>
    <label class="field wide">
      <span>Шрифт</span>
      <input list="font-list" bind:value={el.fontFamily} />
    </label>
    <datalist id="font-list">
      {#each FONTS as f}<option value={f}></option>{/each}
    </datalist>
    <div class="grid2">
      <label class="field">
        <span>Размер, пт</span>
        <input type="number" min="4" max="300" step="0.5" bind:value={el.fontSize} />
      </label>
      <label class="field">
        <span>Интервал</span>
        <input type="number" min="0.7" max="3" step="0.05" bind:value={el.lineHeight} />
      </label>
    </div>
    <div class="row">
      <label class="check"><input type="checkbox" bind:checked={el.bold} /> Жирный</label>
      <label class="check"><input type="checkbox" bind:checked={el.italic} /> Курсив</label>
    </div>
    <div class="segmented" role="radiogroup" aria-label="Выравнивание">
      {#each [['left', 'Слева'], ['center', 'По центру'], ['right', 'Справа']] as [v, t]}
        <label class:on={el.align === v}>
          <input type="radio" name="align" value={v} bind:group={el.align} />{t}
        </label>
      {/each}
    </div>
  {/if}

  {#if el.type === 'image'}
    <label class="field wide">
      <span>Перевод в ч/б</span>
      <select bind:value={el.mode}>
        <option value="dither">Растрирование — для фото и градиентов</option>
        <option value="threshold">Порог — для логотипов и чертежей</option>
      </select>
    </label>
    {#if el.mode === 'threshold'}
      <label class="field wide">
        <span>Порог <output>{el.threshold}</output></span>
        <input type="range" min="1" max="254" bind:value={el.threshold} />
      </label>
    {/if}
    <label class="field wide">
      <span>Яркость <output>{el.brightness}</output></span>
      <input type="range" min="-120" max="120" step="1" bind:value={el.brightness} />
    </label>
    <label class="field wide">
      <span>Контраст <output>{Number(el.contrast).toFixed(2)}</output></span>
      <input type="range" min="0.3" max="3" step="0.05" bind:value={el.contrast} />
    </label>
    <label class="check"><input type="checkbox" bind:checked={el.invert} /> Негатив</label>
  {/if}

  {#if el.type === 'barcode'}
    <label class="field wide">
      <span>Тип</span>
      <select value={el.symbology} onchange={changeSymbology}>
        <optgroup label="Линейные">
          {#each SYMBOLOGIES.filter((s) => !s.twoD) as s}<option value={s.id}>{s.name}</option>{/each}
        </optgroup>
        <optgroup label="Двумерные">
          {#each SYMBOLOGIES.filter((s) => s.twoD) as s}<option value={s.id}>{s.name}</option>{/each}
        </optgroup>
      </select>
    </label>
    <label class="field wide">
      <span>Значение</span>
      {#if sym?.twoD}
        <textarea rows="3" bind:value={el.value} class:invalid={bc?.error}></textarea>
      {:else}
        <input bind:value={el.value} class:invalid={bc?.error} />
      {/if}
    </label>
    {#if bc?.error}<p class="hint error">{bc.error}</p>{/if}
    <div class="grid2">
      <label class="field">
        <span>Модуль, точек</span>
        <input type="number" min="1" max="20" step="1" bind:value={el.module} />
      </label>
      {#if !sym?.twoD}
        <label class="field">
          <span>Высота, мм</span>
          <input type="number" min="1" step="0.5" value={dotsToMm(el.barHeight)}
            onchange={(e) => { const v = parseFloat(e.currentTarget.value); if (v > 0) { el.barHeight = mmToDots(v); ui.dirty = true } }} />
        </label>
      {/if}
    </div>
    <p class="hint">
      Модуль {dotsToMm(el.module * 10) / 10} мм{#if bc && !bc.error}, код {dotsToMm(bc.canvas.width)} × {dotsToMm(bc.canvas.height)} мм{/if}
    </p>
    {#if !sym?.twoD}
      <div class="row">
        <label class="check"><input type="checkbox" bind:checked={el.showText} /> Подпись</label>
        {#if el.showText}
          <label class="field inline">
            <span>Размер, пт</span>
            <input type="number" min="4" max="40" step="0.5" bind:value={el.textSize} />
          </label>
        {/if}
      </div>
    {/if}
  {/if}

  {#if el.type === 'rect' || el.type === 'shape'}
    <label class="check"><input type="checkbox" bind:checked={el.fill} /> Залить черным</label>
    {#if !el.fill}
      <label class="field">
        <span>Толщина линии, точек</span>
        <input type="number" min="1" max="80" step="1" bind:value={el.strokeWidth} />
      </label>
    {/if}
  {/if}

  <div class="grid2 geometry">
    <label class="field"><span>X, мм</span>
      <input type="number" step="0.1" value={dotsToMm(el.x)} onchange={setMm('x')} /></label>
    <label class="field"><span>Y, мм</span>
      <input type="number" step="0.1" value={dotsToMm(el.y)} onchange={setMm('y')} /></label>
    {#if el.type !== 'barcode'}
      <label class="field"><span>Ширина, мм</span>
        <input type="number" step="0.1" min="0.1" value={dotsToMm(el.width)} onchange={setMm('width')} /></label>
    {/if}
    {#if el.type === 'image' || el.type === 'rect' || el.type === 'shape'}
      <label class="field"><span>Высота, мм</span>
        <input type="number" step="0.1" min="0.1" value={dotsToMm(el.height)} onchange={setMm('height')} /></label>
    {/if}
    <label class="field"><span>Поворот, °</span>
      <input type="number" step="90" bind:value={el.rotation} /></label>
  </div>

  <div class="row actions">
    <button onclick={duplicateSelected} title="Ctrl+D">Копия</button>
    <button onclick={() => { if (!moveSelected(1)) showToast('Выше некуда', 'info') }}>Выше</button>
    <button onclick={() => { if (!moveSelected(-1)) showToast('Ниже некуда', 'info') }}>Ниже</button>
    <button class="danger" onclick={removeSelected} title="Delete">Удалить</button>
  </div>
</section>
