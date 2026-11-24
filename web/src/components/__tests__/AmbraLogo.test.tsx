import { render, screen } from '@testing-library/react'
import { describe, expect, it } from 'vitest'
import { AmbraLogo } from '../AmbraLogo'

describe('AmbraLogo 品牌组件', () => {
  it('默认仅渲染 36x36 矢量 SVG 图标，不渲染 Wordmark', () => {
    const { container } = render(<AmbraLogo />)

    const svg = screen.getByLabelText('Ambra Logo')
    expect(svg).toBeInTheDocument()
    expect(svg.tagName.toLowerCase()).toBe('svg')
    expect(svg).toHaveAttribute('width', '36')
    expect(svg).toHaveAttribute('height', '36')
    expect(svg).toHaveAttribute('viewBox', '0 0 128 128')

    // 包含 4 条品牌几何路径与 4 个线性渐变定义
    expect(container.querySelectorAll('path')).toHaveLength(4)
    expect(container.querySelectorAll('linearGradient')).toHaveLength(4)
    expect(screen.queryByText('AMBRA')).not.toBeInTheDocument()
  })

  it('开启 showWordmark 并传入 subtitle 与自定义尺寸时正确渲染', () => {
    render(
      <AmbraLogo
        size={48}
        showWordmark
        subtitle="管理控制台 · Admin Console"
        className="custom-logo"
      />,
    )

    const svg = screen.getByLabelText('Ambra Logo')
    expect(svg).toHaveAttribute('width', '48')
    expect(svg).toHaveAttribute('height', '48')
    expect(screen.getByText('AMBRA')).toBeInTheDocument()
    expect(screen.getByText('管理控制台 · Admin Console')).toBeInTheDocument()
    expect(svg.parentElement).toHaveClass('custom-logo')
  })

  it('同页渲染多个实例时，SVG 渐变 ID 互不冲突', () => {
    const { container } = render(
      <div>
        <AmbraLogo />
        <AmbraLogo showWordmark />
      </div>,
    )

    const gradients = Array.from(container.querySelectorAll('linearGradient'))
    const ids = gradients.map((g) => g.getAttribute('id'))
    expect(ids).toHaveLength(8)
    expect(new Set(ids).size).toBe(8)
  })
})
