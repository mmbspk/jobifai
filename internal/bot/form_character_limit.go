package bot

// A counter belongs to a control only through its description or a local
// wrapper containing that control alone. Page progress, dates and another
// field's counter cannot establish a character constraint.
const jsFormCharacterLimitFn = `
 function charLimitForField(el) {
  if (el.maxLength > 0) return el.maxLength;
  const attr=Number(el.getAttribute('maxlength'));
  if (Number.isInteger(attr)&&attr>0) return attr;
  const counter=node=>{
   if(!node||node.closest('label,header,footer,nav,[role=progressbar]'))return 0;
   const text=(node.textContent||'').trim();
   const match=text.match(/^\d+\s+(?:of)\s+(\d+)\s+characters?\.?$/i)||text.match(/^\d+\s*\/\s*(\d+)(?:\s+characters?)?$/i);
   const cap=match?Number(match[1]):0;return Number.isInteger(cap)&&cap>0&&cap<=5000?cap:0;
  };
  const described=(el.getAttribute('aria-describedby')||'').split(/\s+/).filter(Boolean).map(id=>document.getElementById(id)).map(counter).filter(Boolean);
  if(described.length)return new Set(described).size===1?described[0]:0;
  for(let node=el.parentElement,depth=0;node&&node!==container&&depth<5;node=node.parentElement,depth++){
   const controls=[...node.querySelectorAll('input,textarea,select')].filter(control=>!['hidden','submit','button','reset','image'].includes(control.type));
   if(controls.length!==1||controls[0]!==el)break;
   const caps=[...node.querySelectorAll('*')].filter(node=>node.children.length===0).map(counter).filter(Boolean);
   if(caps.length)return new Set(caps).size===1?caps[0]:0;
  }
  return 0;
 }
`
