/**
 * 失败原因在界面上的显示口径。
 *
 * 后端存的是整条错误链：Go 的 sentinel 是英文（`insights resource is not in a
 * state that allows this action: `），后面才跟着那句写给人看的中文。排查时整条
 * 都有用，摆在页面上却是先一串读不懂的英文、真正的原因躲在后面——人扫一眼
 * 只会以为「系统崩了」，而实际原因可能只是「还没配模型」这种自己就能处理的事。
 *
 * 只在英文前缀确实以「: 」收尾时才切：没有这个形状的（纯中文原因、上游原样
 * 透传的英文报错）整条留着，切错了比多几个字更糟。
 */
export function readableError(raw = ''): string {
  const at = raw.search(/[一-龥]/)
  return at > 0 && /: $/.test(raw.slice(0, at)) ? raw.slice(at) : raw
}

/**
 * 这一次失败是不是「还没配模型」。
 *
 * 这类失败和别的不是一回事：不是数据坏了，也不是模型答错了，是一件人自己十秒钟
 * 就能做完的配置。可界面上只写「还没有配置可用的文本模型」，没说在哪儿配——人
 * 只能挨个入口翻，或者干脆当成系统坏了不再点。
 */
export function isModelSetupFailure(raw = ''): boolean {
  return /模型/.test(raw) && /(没有配置|没配置|未配置|没有可用|不可用)/.test(raw)
}

/** 配模型的地方。全站只有顶栏这一个入口，三处失败提示共用同一句话。 */
export const modelSetupHint = '在顶栏的「模型与密钥设置」里配一个文本模型，再回来重跑。'
