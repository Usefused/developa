import {node,button} from './dom.js';
import {RequestGate} from './api.js';
import {localOptions,optionStep,nextSelectID} from './select-data.js';

export class SearchSelect {
  constructor({label,options = [],selected = null,placeholder = 'Search options…',onChange,loadPage,id}) {
    this.id = id || nextSelectID();
    Object.assign(this,{label,selected,onChange,remote:!!loadPage,opened:false,query:'',items:[],active:-1});
    this.loadPage = loadPage || ((query,offset)=>localOptions(options,query,offset));
    this.gate = new RequestGate();
    this.create(placeholder);
    this.bind();
    this.setSelected(selected);
  }

  create(placeholder) {
    this.element = node('div','search-select');
    this.placeholder = placeholder;
    this.trigger = button('','search-select-trigger',()=>this.toggleOpen());
    this.trigger.id = this.id;
    for (const [name,value] of Object.entries({role:'combobox','aria-label':this.label,'aria-haspopup':'listbox','aria-expanded':'false','aria-controls':`${this.id}-options`})) this.trigger.setAttribute(name,value);
    this.selection = node('span','search-select-value');
    this.trigger.append(this.selection,selectIcon('search-select-chevron','M4 6L8 10L12 6'));
    this.createPopup(placeholder);
    this.element.append(this.trigger,this.popup);
  }

  createPopup(placeholder) {
    this.popup = node('div','search-select-popup');
    this.popup.hidden = true;
    const search = node('div','search-select-search');
    const icon = selectIcon('search-select-icon','M10.5 10.5L14 14M12 7A5 5 0 1 1 2 7A5 5 0 0 1 12 7',18);
    this.input = node('input');
    Object.assign(this.input,{id:`${this.id}-query`,type:'search',placeholder,autocomplete:'off',maxLength:200});
    for (const [name,value] of Object.entries({role:'searchbox','aria-label':`Search ${this.label} options`,'aria-autocomplete':'list','aria-controls':`${this.id}-options`})) this.input.setAttribute(name,value);
    search.append(icon,this.input);
    this.list = node('div','search-select-options');
    this.list.id = `${this.id}-options`;
    this.list.setAttribute('role','listbox');
    this.list.setAttribute('aria-label',this.label);
    this.status = node('p','search-select-status');
    this.status.setAttribute('role','status');
    this.more = button('Load more options','text-button',()=>{ if (!this.loading) this.load(this.nextOffset); });
    this.more.hidden = true;
    this.retry = button('Retry options','text-button',()=>this.load(this.failedOffset));
    this.retry.hidden = true;
    this.more.addEventListener('pointerdown',event=>event.preventDefault());
    this.retry.addEventListener('pointerdown',event=>event.preventDefault());
    this.popup.append(search,this.list,this.status,this.more,this.retry);
  }

  bind() {
    this.trigger.addEventListener('keydown',event=>this.triggerKeydown(event));
    this.input.addEventListener('input',()=>this.search(this.input.value));
    this.input.addEventListener('keydown',event=>this.keydown(event));
    this.element.addEventListener('keydown',event=>{
      if (event.key === 'Escape' && this.opened) {
        event.preventDefault(); event.stopPropagation(); this.close(true);
      }
    });
    this.element.addEventListener('focusout',event=>{
      if (!this.element.contains(event.relatedTarget)) this.close();
    });
    this.outside = event=>{ if (!this.element.contains(event.target)) this.close(); };
  }

  setSelected(item) {
    this.selected = item;
    this.selection.textContent = item?.label || this.placeholder;
    this.selection.classList.toggle('placeholder',!item);
    this.trigger.title = item?.label || this.placeholder;
  }

  toggleOpen() {
    if (this.opened) return this.close(true);
    this.open();
  }

  open() {
    if (this.opened) return;
    this.opened = true;
    this.query = '';
    // Selection and query have different lifetimes. Reopening must never
    // append typed text to a previously selected option's label.
    this.input.value = '';
    this.popup.hidden = false;
    this.trigger.setAttribute('aria-expanded','true');
    document.addEventListener('pointerdown',this.outside);
    this.input.focus({preventScroll:true});
    this.load(0);
  }

  close(restoreFocus = false) {
    if (!this.opened) return;
    this.opened = false;
    this.gate.cancel();
    clearTimeout(this.timer);
    if (restoreFocus) this.trigger.focus({preventScroll:true});
    this.popup.hidden = true;
    this.trigger.setAttribute('aria-expanded','false');
    this.input.removeAttribute('aria-activedescendant');
    document.removeEventListener('pointerdown',this.outside);
  }

  destroy() { this.close(); this.element.remove(); }

  search(query) {
    this.open();
    this.query = query;
    this.gate.cancel();
    clearTimeout(this.timer);
    this.clearOptions();
    this.status.textContent = 'Searching…';
    // Only remote searches are delayed; local settings should filter immediately.
    if (this.remote) this.timer = setTimeout(()=>this.load(0),180);
    else this.load(0);
  }

  clearOptions() {
    this.items = [];
    this.active = -1;
    this.list.replaceChildren();
    this.input.removeAttribute('aria-activedescendant');
    this.more.hidden = true;
    this.retry.hidden = true;
  }

  async load(offset) {
    const request = this.gate.begin();
    this.refocus(this.retry);
    if (!offset) this.clearOptions();
    this.loading = true;
    // Disabling a focused Load more button blurs/closes its own combobox.
    this.more.setAttribute('aria-disabled','true');
    this.retry.hidden = true;
    this.list.setAttribute('aria-busy','true');
    this.status.textContent = 'Loading options…';
    try {
      const page = await this.loadPage(this.query,offset,request.signal);
      if (request.current()) this.accept(page);
    } catch (error) {
      if (request.current() && error.name !== 'AbortError') this.loadError(offset);
    } finally {
      if (request.current()) {
        this.loading = false;
        this.more.setAttribute('aria-disabled','false');
        this.list.setAttribute('aria-busy','false');
      }
    }
  }

  loadError(offset) {
    this.failedOffset = offset;
    this.status.textContent = 'Options could not be loaded. Your selection is unchanged.';
    this.retry.hidden = false;
    this.refocus(this.more);
    this.more.hidden = true;
  }

  refocus(control) {
    if (document.activeElement === control) this.input.focus({preventScroll:true});
  }

  accept(page) {
    const start = this.items.length;
    this.items.push(...page.items);
    page.items.forEach((item,index)=>this.list.append(this.option(item,start+index)));
    this.nextOffset = page.offset+page.items.length;
    const finished = this.nextOffset >= page.total || !page.items.length;
    if (finished) this.refocus(this.more);
    this.more.hidden = finished;
    this.status.textContent = page.total ? `${this.items.length} of ${page.total} options` : 'No matching options.';
    if (this.active < 0) this.highlight(Math.max(0,this.items.findIndex(item=>item.value === this.selected?.value)));
  }

  option(item, index) {
    const option = node('div','search-select-option',item.label);
    option.id = `${this.id}-option-${index}`;
    option.setAttribute('role','option');
    option.setAttribute('aria-selected',String(item.value === this.selected?.value));
    option.addEventListener('pointerdown',event=>event.preventDefault());
    option.addEventListener('click',()=>this.choose(index));
    return option;
  }

  highlight(index) {
    this.active = this.items.length ? index : -1;
    Array.from(this.list.children).forEach((item,position)=>item.classList.toggle('active',position === this.active));
    if (this.active < 0) return this.input.removeAttribute('aria-activedescendant');
    const option = this.list.children[this.active];
    this.input.setAttribute('aria-activedescendant',option.id);
  }

  choose(index) {
    const item = this.items[index];
    if (!item) return;
    this.setSelected(item);
    this.close(true);
    this.onChange(item.value);
  }

  triggerKeydown(event) {
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') {
      event.preventDefault(); this.open(); return;
    }
    if (event.isComposing || event.ctrlKey || event.metaKey || event.altKey || event.key.length !== 1 || event.key === ' ') return;
    event.preventDefault();
    this.open();
    this.input.value = event.key;
    this.search(event.key);
  }

  keydown(event) {
    if (event.isComposing) return;
    if (event.key === 'Escape' && this.opened) {
      event.preventDefault(); event.stopPropagation(); this.close(true); return;
    }
    if (event.key === 'Enter' && this.opened) { event.preventDefault(); this.choose(this.active); return; }
    if (event.key === 'ArrowDown' || event.key === 'ArrowUp') this.arrow(event);
  }

  arrow(event) {
    event.preventDefault();
    if (!this.opened) return this.open();
    this.highlight(optionStep(this.active,event.key === 'ArrowDown' ? 1 : -1,this.items.length));
    this.list.children[this.active]?.scrollIntoView({block:'nearest'});
  }
}

function selectIcon(className, drawing, size = 16) {
  // Vectors keep search and caret icons legible without font-dependent sizing or baselines.
  const namespace = 'http://www.w3.org/2000/svg';
  const svg = document.createElementNS(namespace,'svg');
  const attributes = {class:className,viewBox:'0 0 16 16',width:String(size),height:String(size),'aria-hidden':'true',focusable:'false'};
  for (const [name,value] of Object.entries(attributes)) svg.setAttribute(name,value);
  const path = document.createElementNS(namespace,'path');
  const stroke = {d:drawing,fill:'none',stroke:'currentColor','stroke-width':'1.5','stroke-linecap':'round','stroke-linejoin':'round'};
  for (const [name,value] of Object.entries(stroke)) path.setAttribute(name,value);
  svg.append(path);
  return svg;
}
